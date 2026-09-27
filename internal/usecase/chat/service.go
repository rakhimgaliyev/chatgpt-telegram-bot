package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"chatgpt-telegram-bot/internal/config"
	"chatgpt-telegram-bot/internal/domain"
)

var (
	ErrEmptyMessage  = errors.New("empty message")
	ErrEmptyResponse = errors.New("empty model response")
)

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (string, error)
}

type CompletionRequest struct {
	Model               string
	Messages            []Message
	MaxCompletionTokens int
}

type Message struct {
	Role   string
	Text   string
	Images []string
}

type Input struct {
	Text   string
	Images []Image
}

type Image struct {
	DataURL string
}

type Service struct {
	store  domain.ConversationStore
	client Client
	cfg    config.Config
	now    func() time.Time

	locksMu sync.Mutex
	locks   map[int64]*sync.Mutex
}

func NewService(store domain.ConversationStore, client Client, cfg config.Config) *Service {
	return &Service{
		store:  store,
		client: client,
		cfg:    cfg,
		now:    time.Now,
		locks:  make(map[int64]*sync.Mutex),
	}
}

// chatLock serializes requests of one chat so that each message sees the
// previous turn in its history.
func (s *Service) chatLock(chatID int64) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	l, ok := s.locks[chatID]
	if !ok {
		l = &sync.Mutex{}
		s.locks[chatID] = l
	}
	return l
}

func (s *Service) HandleMessage(ctx context.Context, chatID int64, input Input) (string, error) {
	if strings.TrimSpace(input.Text) == "" && len(input.Images) == 0 {
		return "", ErrEmptyMessage
	}

	lock := s.chatLock(chatID)
	lock.Lock()
	defer lock.Unlock()

	images := make([]string, 0, len(input.Images))
	for _, img := range input.Images {
		images = append(images, img.DataURL)
	}

	userMessage := domain.Message{
		Role:      domain.RoleUser,
		Content:   buildStoredContent(input),
		Images:    images,
		Timestamp: s.now(),
	}

	history := s.store.FreshMessages(chatID, s.cfg.ContextLimit, s.cfg.ContextTTL)

	messages := make([]Message, 0, len(history)+2)
	messages = append(messages, Message{
		Role: domain.RoleSystem,
		Text: s.cfg.AssistantPrompt,
	})
	for _, h := range history {
		messages = append(messages, Message{
			Role:   h.Role,
			Text:   h.Content,
			Images: h.Images,
		})
	}
	messages = append(messages, Message{
		Role:   domain.RoleUser,
		Text:   input.Text,
		Images: images,
	})

	resp, err := s.client.Complete(ctx, CompletionRequest{
		Model:               s.cfg.Model,
		Messages:            messages,
		MaxCompletionTokens: s.cfg.MaxCompletionTokens,
	})
	if err != nil {
		return "", err
	}

	// store the turn only after success so a failed request does not leave
	// a dangling user message in the history
	s.store.Add(chatID, userMessage)
	s.store.Add(chatID, domain.Message{
		Role:      domain.RoleAssistant,
		Content:   resp,
		Timestamp: s.now(),
	})

	return resp, nil
}

// Reset forgets the conversation history of the chat.
func (s *Service) Reset(chatID int64) {
	s.store.Reset(chatID)
}

func buildStoredContent(input Input) string {
	content := strings.TrimSpace(input.Text)
	if len(input.Images) > 0 {
		if content != "" {
			content += "\n"
		}
		content += "[image attached]"
	}
	return content
}
