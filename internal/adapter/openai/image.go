package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	openaiapi "github.com/sashabaranov/go-openai"

	"chatgpt-telegram-bot/internal/usecase/image"
)

func (c *Client) Generate(ctx context.Context, req image.Request) (image.Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return image.Response{}, errors.New("image model is required")
	}

	format := strings.TrimSpace(req.Format)
	if format == "" {
		format = "png"
	}

	resp, err := c.api.CreateImage(ctx, openaiapi.ImageRequest{
		Model:        req.Model,
		Prompt:       req.Prompt,
		N:            1,
		Size:         strings.TrimSpace(req.Size),
		Quality:      strings.TrimSpace(req.Quality),
		OutputFormat: format,
		Background:   strings.TrimSpace(req.Background),
	})
	if err != nil {
		return image.Response{}, err
	}

	for _, out := range resp.Data {
		if strings.TrimSpace(out.B64JSON) == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(out.B64JSON)
		if err != nil {
			return image.Response{}, err
		}
		return image.Response{Data: data, Format: format}, nil
	}

	return image.Response{}, errors.New("no image in response")
}
