package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	openaiapi "github.com/sashabaranov/go-openai"

	"chatgpt-telegram-bot/internal/usecase/image"
)

// go-openai's edit request accepts a single image and lacks output_format
// and background, so edits are sent as a hand-built multipart request.
const imageEditsEndpoint = "https://api.openai.com/v1/images/edits"

func (c *Client) Generate(ctx context.Context, req image.Request) (image.Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return image.Response{}, errors.New("image model is required")
	}

	format := strings.TrimSpace(req.Format)
	if format == "" {
		format = "png"
	}

	var (
		data []openaiapi.ImageResponseDataInner
		err  error
	)
	if len(req.Images) > 0 {
		data, err = c.editImage(ctx, req, format)
	} else {
		data, err = c.createImage(ctx, req, format)
	}
	if err != nil {
		return image.Response{}, err
	}

	for _, out := range data {
		if strings.TrimSpace(out.B64JSON) == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(out.B64JSON)
		if err != nil {
			return image.Response{}, err
		}
		return image.Response{Data: raw, Format: format}, nil
	}

	return image.Response{}, errors.New("no image in response")
}

func (c *Client) createImage(ctx context.Context, req image.Request, format string) ([]openaiapi.ImageResponseDataInner, error) {
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
		var apiErr *openaiapi.APIError
		if errors.As(err, &apiErr) && isModerationError(fmt.Sprint(apiErr.Code), apiErr.Message) {
			return nil, fmt.Errorf("%w: %s", image.ErrModerationBlocked, apiErr.Message)
		}
		return nil, err
	}
	return resp.Data, nil
}

func isModerationError(code, message string) bool {
	return code == "moderation_blocked" || strings.Contains(message, "safety system")
}

func (c *Client) editImage(ctx context.Context, req image.Request, format string) ([]openaiapi.ImageResponseDataInner, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	fields := map[string]string{
		"model":         req.Model,
		"prompt":        req.Prompt,
		"n":             "1",
		"size":          strings.TrimSpace(req.Size),
		"quality":       strings.TrimSpace(req.Quality),
		"output_format": format,
		"background":    strings.TrimSpace(req.Background),
	}
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return nil, err
		}
	}

	for i, img := range req.Images {
		ext, ok := imageExtensions[img.MimeType]
		if !ok {
			return nil, fmt.Errorf("unsupported input image type %q", img.MimeType)
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="image%d.%s"`, i, ext))
		h.Set("Content-Type", img.MimeType)
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(img.Data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, imageEditsEndpoint, &body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Data  []openaiapi.ImageResponseDataInner `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil && resp.StatusCode == http.StatusOK {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && isModerationError(parsed.Error.Code, parsed.Error.Message) {
			return nil, fmt.Errorf("%w: %s", image.ErrModerationBlocked, parsed.Error.Message)
		}
		if parsed.Error != nil && parsed.Error.Message != "" {
			return nil, fmt.Errorf("openai image edit: %s", parsed.Error.Message)
		}
		return nil, fmt.Errorf("openai image edit: status %d", resp.StatusCode)
	}
	return parsed.Data, nil
}

// imageExtensions lists the input formats accepted by the edits endpoint.
var imageExtensions = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
}
