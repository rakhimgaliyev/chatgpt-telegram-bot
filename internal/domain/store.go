package domain

import "time"

type ConversationStore interface {
	Add(chatID int64, msg Message)
	FreshMessages(chatID int64, limit int, ttl time.Duration) []Message
}
