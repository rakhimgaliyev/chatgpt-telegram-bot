package telegram

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"chatgpt-telegram-bot/internal/config"
	"chatgpt-telegram-bot/internal/usecase/chat"
)

type Bot struct {
	api  *tgbotapi.BotAPI
	cfg  config.Config
	chat *chat.Service
	now  func() time.Time
}

func NewBot(cfg config.Config, chatSvc *chat.Service) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:  api,
		cfg:  cfg,
		chat: chatSvc,
		now:  time.Now,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			msg := update.Message
			if msg.From == nil {
				continue
			}
			go b.handleMessage(ctx, msg)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if !isAllowedUser(msg.From.ID, b.cfg) {
		deny := tgbotapi.NewMessage(msg.Chat.ID, "access denied")
		deny.ReplyToMessageID = msg.MessageID
		if _, err := b.api.Send(deny); err != nil {
			log.Printf("failed to send deny message: %v", err)
		}
		return
	}

	userInput, respondAsFile := BuildUserInput(b.api, msg)
	b.sendChatAction(msg.Chat.ID, respondAsFile)

	resp, err := b.chat.HandleMessage(ctx, msg.Chat.ID, userInput)
	if err != nil {
		if errors.Is(err, chat.ErrEmptyMessage) {
			b.sendText(msg.Chat.ID, msg.MessageID, "i need some content to work with")
			return
		}
		log.Printf("openai request failed: %v", err)
		b.sendText(msg.Chat.ID, msg.MessageID, "failed to reach openai, try again later")
		return
	}

	if respondAsFile {
		if err := b.sendAsFile(msg.Chat.ID, msg.MessageID, resp); err != nil {
			log.Printf("failed to send file: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not send file, here is the text")
			b.sendText(msg.Chat.ID, msg.MessageID, resp)
		}
		return
	}

	if shouldSendAsFile(resp) {
		if err := b.sendAsFile(msg.Chat.ID, msg.MessageID, resp); err != nil {
			log.Printf("failed to send file: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not send file, here is the text")
			b.sendText(msg.Chat.ID, msg.MessageID, resp)
		}
		return
	}

	b.sendText(msg.Chat.ID, msg.MessageID, resp)
}

func (b *Bot) sendText(chatID int64, replyTo int, text string) {
	const chunkSize = 2048

	chunks := splitText(text, chunkSize)
	for idx, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = tgbotapi.ModeMarkdown
		if idx == 0 {
			msg.ReplyToMessageID = replyTo
		}
		if _, err := b.api.Send(msg); err != nil {
			log.Printf("failed to send reply: %v", err)
		}
	}
}

func (b *Bot) sendChatAction(chatID int64, asFile bool) {
	action := tgbotapi.ChatTyping
	if asFile {
		action = tgbotapi.ChatUploadDocument
	}
	if _, err := b.api.Request(tgbotapi.NewChatAction(chatID, action)); err != nil {
		log.Printf("failed to send chat action: %v", err)
	}
}

func (b *Bot) sendAsFile(chatID int64, replyTo int, content string) error {
	data := []byte(content)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  "response.md",
		Bytes: data,
	})
	doc.ReplyToMessageID = replyTo

	_, err := b.api.Send(doc)
	return err
}

func shouldSendAsFile(text string) bool {
	const chunkSize = 2048
	return len([]rune(text)) > chunkSize
}

func isAllowedUser(userID int64, cfg config.Config) bool {
	for _, id := range cfg.AdminUserIDs {
		if id == userID {
			return true
		}
	}

	if len(cfg.AllowedUserIDs) == 0 {
		return true
	}

	for _, id := range cfg.AllowedUserIDs {
		if id == userID {
			return true
		}
	}

	return false
}

func BuildUserInput(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) (chat.Input, bool) {
	respondAsFile := false
	text := msg.Text
	if strings.HasPrefix(strings.ToLower(text), "/file") {
		respondAsFile = true
		text = strings.TrimSpace(text[len("/file"):])
	}

	parts := make([]string, 0, 6)
	if text != "" {
		parts = append(parts, text)
	}
	if msg.Caption != "" {
		parts = append(parts, "Caption: "+msg.Caption)
	}

	attachmentParts, images := DescribeAttachments(bot, msg)
	parts = append(parts, attachmentParts...)

	return chat.Input{
		Text:   strings.Join(parts, "\n"),
		Images: images,
	}, respondAsFile
}

func splitText(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		return []string{text}
	}

	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}

	chunks := make([]string, 0, len(runes)/chunkSize+1)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}

	return chunks
}
