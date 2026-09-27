package telegram

import (
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestSplitAlbumPicksCaption(t *testing.T) {
	msgs := []*tgbotapi.Message{{MessageID: 1}, {MessageID: 2, Caption: "what is common?"}, {MessageID: 3}}
	primary, rest := splitAlbum(msgs)
	if primary.MessageID != 2 || len(rest) != 2 {
		t.Errorf("primary %d, rest %d", primary.MessageID, len(rest))
	}
}

func TestIsCommandForOtherBot(t *testing.T) {
	cases := map[string]bool{
		"/help":            false,
		"/help@my_bot":     false,
		"/help@My_Bot hi":  false,
		"/help@other_bot":  true,
		"hello @other_bot": false,
		"":                 false,
	}
	for text, want := range cases {
		if got := isCommandForOtherBot(text, "my_bot"); got != want {
			t.Errorf("isCommandForOtherBot(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Now()
	r := newRateLimiter(2, time.Hour)
	r.now = func() time.Time { return now }
	if !r.allow(1) || !r.allow(1) {
		t.Fatal("first two requests must pass")
	}
	if r.allow(1) {
		t.Error("third request within the window must be blocked")
	}
	if !r.allow(2) {
		t.Error("limit is per user")
	}
	now = now.Add(time.Hour + time.Second)
	if !r.allow(1) {
		t.Error("requests must pass again after the window")
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		u    *tgbotapi.User
		want string
	}{
		{&tgbotapi.User{FirstName: "Ann", LastName: "Lee", UserName: "ann"}, "Ann Lee (@ann)"},
		{&tgbotapi.User{UserName: "ann"}, "@ann"},
		{&tgbotapi.User{FirstName: "Ann"}, "Ann"},
		{nil, "unknown"},
	}
	for _, c := range cases {
		if got := displayName(c.u); got != c.want {
			t.Errorf("displayName = %q, want %q", got, c.want)
		}
	}
}

func TestAttachmentHelpers(t *testing.T) {
	if mediaFilename("audio", "audio/mpeg") != "audio.mp3" || mediaFilename("video", "video/quicktime") != "" {
		t.Error("mediaFilename mapping is wrong")
	}
	if !isTextDocument("main.go", "application/octet-stream") || !isTextDocument("x", "text/plain") || isTextDocument("a.pdf", "application/pdf") {
		t.Error("isTextDocument is wrong")
	}
	if truncateRunes("привет", 3) != "при…" || truncateRunes("hi", 3) != "hi" {
		t.Error("truncateRunes is wrong")
	}
}
