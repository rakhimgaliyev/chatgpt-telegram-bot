package image

import (
	"context"
	"errors"
	"strings"

	"chatgpt-telegram-bot/internal/config"
)

var ErrEmptyPrompt = errors.New("empty prompt")

type Client interface {
	Generate(ctx context.Context, req Request) (Response, error)
}

type Request struct {
	Model      string
	Prompt     string
	Size       string
	Quality    string
	Format     string
	Background string
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

func (s *Service) Generate(ctx context.Context, prompt string) (Response, error) {
	if strings.TrimSpace(prompt) == "" {
		return Response{}, ErrEmptyPrompt
	}

	return s.client.Generate(ctx, Request{
		Model:      s.cfg.ImageModel,
		Prompt:     prompt,
		Size:       s.cfg.ImageSize,
		Quality:    s.cfg.ImageQuality,
		Format:     s.cfg.ImageFormat,
		Background: s.cfg.ImageBackground,
	})
}
