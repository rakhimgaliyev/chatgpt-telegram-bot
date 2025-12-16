package memory

import (
	"sync"
	"time"

	"chatgpt-telegram-bot/internal/domain"
)

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
	s.conversations[chatID] = append(s.conversations[chatID], msg)
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

	if len(fresh) > limit {
		fresh = fresh[len(fresh)-limit:]
	}

	return append([]domain.Message(nil), fresh...)
}
