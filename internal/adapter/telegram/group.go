package telegram

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// albumDelay is how long to wait for the rest of an album: Telegram sends
// every photo of a media group as a separate message.
const albumDelay = 1500 * time.Millisecond

type albumCollector struct {
	mu      sync.Mutex
	pending map[string][]*tgbotapi.Message
	flush   func([]*tgbotapi.Message)
}

func newAlbumCollector(flush func([]*tgbotapi.Message)) *albumCollector {
	return &albumCollector{
		pending: make(map[string][]*tgbotapi.Message),
		flush:   flush,
	}
}

func (a *albumCollector) add(msg *tgbotapi.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()

	id := msg.MediaGroupID
	if _, ok := a.pending[id]; !ok {
		time.AfterFunc(albumDelay, func() {
			a.mu.Lock()
			msgs := a.pending[id]
			delete(a.pending, id)
			a.mu.Unlock()
			a.flush(msgs)
		})
	}
	a.pending[id] = append(a.pending[id], msg)
}

// splitAlbum picks the message carrying the caption as the primary one,
// the caption is where users put the question or command.
func splitAlbum(msgs []*tgbotapi.Message) (*tgbotapi.Message, []*tgbotapi.Message) {
	primary := 0
	for i, m := range msgs {
		if strings.TrimSpace(m.Caption) != "" {
			primary = i
			break
		}
	}
	rest := make([]*tgbotapi.Message, 0, len(msgs)-1)
	for i, m := range msgs {
		if i != primary {
			rest = append(rest, m)
		}
	}
	return msgs[primary], rest
}

// isAddressed reports whether a group message is meant for the bot: a
// command, a mention of the bot or a reply to one of its messages.
func (b *Bot) isAddressed(msg *tgbotapi.Message, text string) bool {
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		return true
	}
	if r := msg.ReplyToMessage; r != nil && r.From != nil && r.From.ID == b.api.Self.ID {
		return true
	}
	return b.api.Self.UserName != "" &&
		strings.Contains(strings.ToLower(text), "@"+strings.ToLower(b.api.Self.UserName))
}

// stripMention removes "@botname" so the model does not see it as content.
func (b *Bot) stripMention(text string) string {
	if b.api.Self.UserName == "" {
		return text
	}
	re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(b.api.Self.UserName) + `\b`)
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

// isCommandForOtherBot reports whether text is a "/cmd@otherbot" command.
func isCommandForOtherBot(text, botUsername string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return false
	}
	_, target, ok := strings.Cut(strings.Fields(text)[0], "@")
	return ok && !strings.EqualFold(target, botUsername)
}

func displayName(u *tgbotapi.User) string {
	if u == nil {
		return "unknown"
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.UserName != "" {
		if name == "" {
			return "@" + u.UserName
		}
		return name + " (@" + u.UserName + ")"
	}
	if name == "" {
		return "unknown"
	}
	return name
}

// rateLimiter allows at most limit events per user within window.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[int64][]time.Time
	now    func() time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:  limit,
		window: window,
		hits:   make(map[int64][]time.Time),
		now:    time.Now,
	}
}

func (r *rateLimiter) allow(userID int64) bool {
	if r.limit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := r.now().Add(-r.window)
	recent := r.hits[userID][:0]
	for _, t := range r.hits[userID] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= r.limit {
		r.hits[userID] = recent
		return false
	}
	r.hits[userID] = append(recent, r.now())
	return true
}

// keepAction shows a chat action ("typing", "sending photo") until the
// returned stop function is called; Telegram clears an action after 5s.
func (b *Bot) keepAction(ctx context.Context, chatID int64, action string) func() {
	ctx, cancel := context.WithCancel(ctx)
	send := func() {
		if _, err := b.api.Request(tgbotapi.NewChatAction(chatID, action)); err != nil {
			log.Printf("failed to send chat action: %v", err)
		}
	}
	send()
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				send()
			}
		}
	}()
	return cancel
}
