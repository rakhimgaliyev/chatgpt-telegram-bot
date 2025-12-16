package chat

import (
	"context"
	"errors"
	"strings"
	"time"

	"chatgpt-telegram-bot/internal/config"
	"chatgpt-telegram-bot/internal/domain"
)

var ErrEmptyMessage = errors.New("empty message")

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
}

func NewService(store domain.ConversationStore, client Client, cfg config.Config) *Service {
	return &Service{
		store:  store,
		client: client,
		cfg:    cfg,
		now:    time.Now,
	}
}

func (s *Service) HandleMessage(ctx context.Context, chatID int64, input Input) (string, error) {
	if strings.TrimSpace(input.Text) == "" && len(input.Images) == 0 {
		return "", ErrEmptyMessage
	}

	userMessage := domain.Message{
		Role:      domain.RoleUser,
		Content:   buildStoredContent(input),
		Timestamp: s.now(),
	}

	history := s.store.FreshMessages(chatID, s.cfg.ContextLimit, s.cfg.ContextTTL)
	s.store.Add(chatID, userMessage)

	messages := make([]Message, 0, len(history)+2)
	messages = append(messages, Message{
		Role: domain.RoleSystem,
		Text: s.cfg.AssistantPrompt,
	})
	for _, h := range history {
		messages = append(messages, Message{
			Role: h.Role,
			Text: h.Content,
		})
	}
	userParts := Message{
		Role: domain.RoleUser,
		Text: input.Text,
	}
	for _, img := range input.Images {
		userParts.Images = append(userParts.Images, img.DataURL)
	}
	messages = append(messages, userParts)

	resp, err := s.client.Complete(ctx, CompletionRequest{
		Model:               s.cfg.Model,
		Messages:            messages,
		MaxCompletionTokens: s.cfg.MaxCompletionTokens,
	})
	if err != nil {
		return "", err
	}

	s.store.Add(chatID, domain.Message{
		Role:      domain.RoleAssistant,
		Content:   resp,
		Timestamp: s.now(),
	})

	return resp, nil
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
