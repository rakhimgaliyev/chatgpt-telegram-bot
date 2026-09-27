package domain

import "time"

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	Role      string
	Content   string
	Images    []string // data URLs, kept only for the most recent messages
	Timestamp time.Time
}
