package transcribe

import (
	"context"
	"errors"

	"chatgpt-telegram-bot/internal/config"
)

var ErrEmptyAudio = errors.New("empty audio")

type Client interface {
	Transcribe(ctx context.Context, req Request) (string, error)
}

type Request struct {
	Model string
	// Filename carries the extension the API uses to detect the format.
	Filename string
	Data     []byte
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

func (s *Service) Transcribe(ctx context.Context, filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrEmptyAudio
	}
	return s.client.Transcribe(ctx, Request{
		Model:    s.cfg.TranscribeModel,
		Filename: filename,
		Data:     data,
	})
}
