package memory

import (
	"sync"
	"time"

	"chatgpt-telegram-bot/internal/domain"
)

// maxImageMessages bounds how many messages per chat keep their image
// data; older ones keep only the text so memory and token usage stay small.
const maxImageMessages = 3

type Store struct {
	mu            sync.Mutex
	conversations map[int64][]domain.Message
}

func NewStore() *Store {
	return &Store{
		conversations: make(map[int64][]domain.Message),
	}
}

func (s *Store) Add(chatID int64, msg domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := append(s.conversations[chatID], msg)

	withImages := 0
	for i := len(history) - 1; i >= 0; i-- {
		if len(history[i].Images) == 0 {
			continue
		}
		withImages++
		if withImages > maxImageMessages {
			history[i].Images = nil
		}
	}
	s.conversations[chatID] = history
}

func (s *Store) Reset(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conversations, chatID)
}

func (s *Store) FreshMessages(chatID int64, limit int, ttl time.Duration) []domain.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	history := s.conversations[chatID]
	if len(history) == 0 {
		return nil
	}

	cutoff := time.Now().Add(-ttl)
	fresh := make([]domain.Message, 0, len(history))
	for _, m := range history {
		if m.Timestamp.After(cutoff) {
			fresh = append(fresh, m)
		}
	}

	if limit >= 0 && len(fresh) > limit {
		fresh = fresh[len(fresh)-limit:]
	}

	// expired and over-limit messages are never read again, drop them so
	// memory does not grow for the lifetime of the process
	if len(fresh) == 0 {
		delete(s.conversations, chatID)
		return nil
	}
	s.conversations[chatID] = fresh

	return append([]domain.Message(nil), fresh...)
}
