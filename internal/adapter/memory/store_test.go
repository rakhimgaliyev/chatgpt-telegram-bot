package memory

import (
	"testing"
	"time"

	"chatgpt-telegram-bot/internal/domain"
)

func TestFreshMessagesPrunes(t *testing.T) {
	s := NewStore()
	now := time.Now()
	s.Add(1, domain.Message{Content: "old", Timestamp: now.Add(-2 * time.Hour)})
	for i := 0; i < 5; i++ {
		s.Add(1, domain.Message{Content: "new", Timestamp: now})
	}

	got := s.FreshMessages(1, 3, time.Hour)
	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3", len(got))
	}
	if n := len(s.conversations[1]); n != 3 {
		t.Errorf("store kept %d messages, want 3", n)
	}

	s.Add(2, domain.Message{Content: "old", Timestamp: now.Add(-2 * time.Hour)})
	if got := s.FreshMessages(2, 3, time.Hour); got != nil {
		t.Errorf("expected no fresh messages, got %v", got)
	}
	if _, ok := s.conversations[2]; ok {
		t.Error("expired chat must be removed from the store")
	}
}
