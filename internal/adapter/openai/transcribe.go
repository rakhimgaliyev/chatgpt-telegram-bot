package openai

import (
	"bytes"
	"context"
	"strings"

	openaiapi "github.com/sashabaranov/go-openai"

	"chatgpt-telegram-bot/internal/usecase/transcribe"
)

func (c *Client) Transcribe(ctx context.Context, req transcribe.Request) (string, error) {
	resp, err := c.api.CreateTranscription(ctx, openaiapi.AudioRequest{
		Model:    req.Model,
		FilePath: req.Filename,
		Reader:   bytes.NewReader(req.Data),
		Format:   openaiapi.AudioResponseFormatJSON,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text), nil
}
