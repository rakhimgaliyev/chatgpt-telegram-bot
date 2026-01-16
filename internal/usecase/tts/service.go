package tts

import (
	"context"
	"errors"
	"strings"

	"chatgpt-telegram-bot/internal/config"
)

var ErrEmptyText = errors.New("empty text")

type Client interface {
	Speech(ctx context.Context, req Request) (Response, error)
}

type Request struct {
	Model  string
	Voice  string
	Format string
	Text   string
}

type Response struct {
	Data   []byte
	Format string
}

type Service struct {
	client Client
	cfg    config.Config
}

func NewService(client Client, cfg config.Config) *Service {
	return &Service{
		client: client,
		cfg:    cfg,
	}
}

func (s *Service) Synthesize(ctx context.Context, text string) (Response, error) {
	if strings.TrimSpace(text) == "" {
		return Response{}, ErrEmptyText
	}

	return s.client.Speech(ctx, Request{
		Model:  s.cfg.TTSModel,
		Voice:  s.cfg.TTSVoice,
		Format: s.cfg.TTSFormat,
		Text:   text,
	})
}
