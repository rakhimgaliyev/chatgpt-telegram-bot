package telegram

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestExtractCommandText(t *testing.T) {
	cases := []struct {
		text, cmd string
		ok        bool
		rest      string
	}{
		{"/file hello", "file", true, "hello"},
		{"/file@my_bot hello", "file", true, "hello"},
		{"  /FILE hello", "file", true, "hello"},
		{"/filed hello", "file", false, ""},
		{"hello /file", "file", false, ""},
		{"/img", "img", true, ""},
	}
	for _, c := range cases {
		ok, rest := extractCommandText(c.text, c.cmd)
		if ok != c.ok || rest != c.rest {
			t.Errorf("extractCommandText(%q, %q) = %v, %q; want %v, %q", c.text, c.cmd, ok, rest, c.ok, c.rest)
		}
	}
}

func TestHasUserContent(t *testing.T) {
	if hasUserContent(&tgbotapi.Message{}) {
		t.Error("service message without content must be ignored")
	}
	if !hasUserContent(&tgbotapi.Message{Text: "hi"}) {
		t.Error("text message must be handled")
	}
	if !hasUserContent(&tgbotapi.Message{Photo: []tgbotapi.PhotoSize{{}}}) {
		t.Error("photo message must be handled")
	}
}

func TestUnknownCommand(t *testing.T) {
	cases := []struct {
		text     string
		wantName string
		wantOK   bool
	}{
		{"hello", "", false},
		{"/img cat", "", false},
		{"/help", "", false},
		{"/stats", "stats", true},
		{"/resend@my_bot", "resend", true},
		{"/stats@other_bot", "", true},
	}
	for _, c := range cases {
		name, ok := unknownCommand(c.text, "my_bot")
		if name != c.wantName || ok != c.wantOK {
			t.Errorf("unknownCommand(%q) = %q, %v; want %q, %v", c.text, name, ok, c.wantName, c.wantOK)
		}
	}
}
