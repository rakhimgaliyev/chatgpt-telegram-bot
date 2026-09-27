package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"chatgpt-telegram-bot/internal/usecase/image"
)

func TestEditImageMultipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("authorization = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		for k, want := range map[string]string{"model": "gpt-image", "prompt": "add a hat", "output_format": "png", "n": "1"} {
			if got := r.FormValue(k); got != want {
				t.Errorf("field %s = %q, want %q", k, got, want)
			}
		}
		if _, ok := r.MultipartForm.Value["background"]; ok {
			t.Error("empty fields must not be sent")
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) != 2 {
			t.Fatalf("got %d images, want 2", len(files))
		}
		if ct := files[1].Header.Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("second image content type = %q", ct)
		}
		f, _ := files[0].Open()
		data, _ := io.ReadAll(f)
		if string(data) != "png-bytes" {
			t.Errorf("first image data = %q", data)
		}
		io.WriteString(w, `{"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString([]byte("result"))+`"}]}`)
	}))
	defer srv.Close()

	c := NewClient("key")
	c.imageEditsURL = srv.URL
	resp, err := c.Generate(context.Background(), image.Request{
		Model:  "gpt-image",
		Prompt: "add a hat",
		Images: []image.InputImage{
			{Data: []byte("png-bytes"), MimeType: "image/png"},
			{Data: []byte("jpeg-bytes"), MimeType: "image/jpeg"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Data) != "result" || resp.Format != "png" {
		t.Errorf("got %q %q", resp.Data, resp.Format)
	}
}

func TestEditImageModeration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"Your request was rejected by the safety system.","code":"moderation_blocked"}}`)
	}))
	defer srv.Close()

	c := NewClient("key")
	c.imageEditsURL = srv.URL
	_, err := c.Generate(context.Background(), image.Request{
		Model:  "gpt-image",
		Prompt: "x",
		Images: []image.InputImage{{Data: []byte("x"), MimeType: "image/png"}},
	})
	if !errors.Is(err, image.ErrModerationBlocked) {
		t.Errorf("err = %v, want ErrModerationBlocked", err)
	}
}
