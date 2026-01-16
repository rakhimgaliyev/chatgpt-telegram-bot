package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"chatgpt-telegram-bot/internal/usecase/image"
)

const responsesEndpoint = "https://api.openai.com/v1/responses"

type responsesCreateRequest struct {
	Model      string               `json:"model"`
	Input      string               `json:"input"`
	Tools      []responsesImageTool `json:"tools,omitempty"`
	ToolChoice map[string]string    `json:"tool_choice,omitempty"`
}

type responsesImageTool struct {
	Type       string `json:"type"`
	Size       string `json:"size,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Format     string `json:"format,omitempty"`
	Background string `json:"background,omitempty"`
}

type responsesCreateResponse struct {
	Output []responsesOutputItem `json:"output"`
	Error  *responsesError       `json:"error,omitempty"`
}

type responsesOutputItem struct {
	Type   string `json:"type"`
	Result string `json:"result"`
}

type responsesError struct {
	Message string `json:"message"`
}

func (c *Client) Generate(ctx context.Context, req image.Request) (image.Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return image.Response{}, errors.New("image model is required")
	}

	tool := responsesImageTool{
		Type:       "image_generation",
		Size:       strings.TrimSpace(req.Size),
		Quality:    strings.TrimSpace(req.Quality),
		Format:     strings.TrimSpace(req.Format),
		Background: strings.TrimSpace(req.Background),
	}

	createReq := responsesCreateRequest{
		Model:      req.Model,
		Input:      req.Prompt,
		Tools:      []responsesImageTool{tool},
		ToolChoice: map[string]string{"type": "image_generation"},
	}

	body, err := json.Marshal(createReq)
	if err != nil {
		return image.Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, responsesEndpoint, bytes.NewReader(body))
	if err != nil {
		return image.Response{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return image.Response{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return image.Response{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr responsesCreateResponse
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error != nil && apiErr.Error.Message != "" {
			return image.Response{}, fmt.Errorf("openai error: %s", apiErr.Error.Message)
		}
		return image.Response{}, fmt.Errorf("openai error: status %d", resp.StatusCode)
	}

	var apiResp responsesCreateResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return image.Response{}, err
	}

	for _, out := range apiResp.Output {
		if out.Type != "image_generation_call" || strings.TrimSpace(out.Result) == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(out.Result)
		if err != nil {
			return image.Response{}, err
		}
		format := strings.TrimSpace(req.Format)
		if format == "" {
			format = "png"
		}
		return image.Response{Data: data, Format: format}, nil
	}

	return image.Response{}, errors.New("no image generation result in response")
}
