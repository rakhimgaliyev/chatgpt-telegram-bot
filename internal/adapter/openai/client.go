package openai

import (
	"context"
	"errors"
	"io"
	"strings"

	openaiapi "github.com/sashabaranov/go-openai"

	"chatgpt-telegram-bot/internal/usecase/chat"
	"chatgpt-telegram-bot/internal/usecase/tts"
)

type Client struct {
	api *openaiapi.Client
}

func NewClient(token string) *Client {
	return &Client{
		api: openaiapi.NewClient(token),
	}
}

func (c *Client) Complete(ctx context.Context, req chat.CompletionRequest) (string, error) {
	apiReq := openaiapi.ChatCompletionRequest{
		Model:               req.Model,
		MaxCompletionTokens: req.MaxCompletionTokens,
		Stream:              false,
		Messages:            toAPIMessages(req.Messages),
	}

	resp, err := c.api.CreateChatCompletion(ctx, apiReq)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", errors.New("openai returned empty response")
	}

	return resp.Choices[0].Message.Content, nil
}

func (c *Client) Speech(ctx context.Context, req tts.Request) (tts.Response, error) {
	format := strings.TrimSpace(req.Format)
	if format == "" {
		format = "mp3"
	}

	apiReq := openaiapi.CreateSpeechRequest{
		Model: openaiapi.SpeechModel(req.Model),
		Input: req.Text,
		Voice: openaiapi.SpeechVoice(req.Voice),
	}
	if req.Format != "" {
		apiReq.ResponseFormat = openaiapi.SpeechResponseFormat(req.Format)
	}

	raw, err := c.api.CreateSpeech(ctx, apiReq)
	if err != nil {
		return tts.Response{}, err
	}
	defer raw.Close()

	data, err := io.ReadAll(raw)
	if err != nil {
		return tts.Response{}, err
	}

	return tts.Response{
		Data:   data,
		Format: format,
	}, nil
}

func toAPIMessages(msgs []chat.Message) []openaiapi.ChatCompletionMessage {
	res := make([]openaiapi.ChatCompletionMessage, 0, len(msgs))
	for _, m := range msgs {
		if len(m.Images) == 0 {
			res = append(res, openaiapi.ChatCompletionMessage{
				Role:    m.Role,
				Content: m.Text,
			})
			continue
		}

		parts := make([]openaiapi.ChatMessagePart, 0, len(m.Images)+1)
		if strings.TrimSpace(m.Text) != "" {
			parts = append(parts, openaiapi.ChatMessagePart{
				Type: openaiapi.ChatMessagePartTypeText,
				Text: m.Text,
			})
		}
		for _, img := range m.Images {
			parts = append(parts, openaiapi.ChatMessagePart{
				Type: openaiapi.ChatMessagePartTypeImageURL,
				ImageURL: &openaiapi.ChatMessageImageURL{
					URL:    img,
					Detail: openaiapi.ImageURLDetailAuto,
				},
			})
		}

		res = append(res, openaiapi.ChatCompletionMessage{
			Role:         m.Role,
			MultiContent: parts,
		})
	}
	return res
}
