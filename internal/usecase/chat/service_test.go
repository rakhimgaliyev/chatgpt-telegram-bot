package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"chatgpt-telegram-bot/internal/adapter/memory"
	"chatgpt-telegram-bot/internal/config"
)

type fakeClient struct {
	reqs []CompletionRequest
	resp string
	err  error
}

func (f *fakeClient) Complete(_ context.Context, req CompletionRequest) (string, error) {
	f.reqs = append(f.reqs, req)
	return f.resp, f.err
}

func newTestService(client Client) *Service {
	return NewService(memory.NewStore(), client, config.Config{
		AssistantPrompt: "system",
		ContextLimit:    20,
		ContextTTL:      time.Hour,
	})
}

func TestHandleMessageKeepsHistoryAndImages(t *testing.T) {
	client := &fakeClient{resp: "answer"}
	svc := newTestService(client)
	ctx := context.Background()

	if _, err := svc.HandleMessage(ctx, 1, Input{Text: "look", Images: []Image{{DataURL: "data:image/png;base64,AA"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.HandleMessage(ctx, 1, Input{Text: "what is on the right?"}); err != nil {
		t.Fatal(err)
	}

	msgs := client.reqs[1].Messages
	// system, user with image, assistant, new user
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4", len(msgs))
	}
	if len(msgs[1].Images) != 1 {
		t.Error("image from the previous turn must stay in context")
	}
	if msgs[2].Role != "assistant" || msgs[2].Text != "answer" {
		t.Errorf("unexpected assistant message %+v", msgs[2])
	}
}

func TestHandleMessageFailureNotStored(t *testing.T) {
	client := &fakeClient{err: errors.New("boom")}
	svc := newTestService(client)
	ctx := context.Background()

	if _, err := svc.HandleMessage(ctx, 1, Input{Text: "first"}); err == nil {
		t.Fatal("expected error")
	}
	client.err, client.resp = nil, "ok"
	if _, err := svc.HandleMessage(ctx, 1, Input{Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if n := len(client.reqs[1].Messages); n != 2 {
		t.Errorf("failed turn leaked into history: %d messages", n)
	}
}

func TestReset(t *testing.T) {
	client := &fakeClient{resp: "ok"}
	svc := newTestService(client)
	ctx := context.Background()
	svc.HandleMessage(ctx, 1, Input{Text: "a"})
	svc.Reset(1)
	svc.HandleMessage(ctx, 1, Input{Text: "b"})
	if n := len(client.reqs[1].Messages); n != 2 {
		t.Errorf("history not cleared: %d messages", n)
	}
}
