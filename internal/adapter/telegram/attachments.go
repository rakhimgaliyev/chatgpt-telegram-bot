package telegram

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"chatgpt-telegram-bot/internal/usecase/chat"
)

func DescribeAttachments(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) ([]string, []chat.Image) {
	parts := make([]string, 0, 8)
	images := make([]chat.Image, 0, 4)

	if msg.Document != nil {
		part, img := describeDocument(bot, msg.Document)
		parts = append(parts, part)
		if img.DataURL != "" {
			images = append(images, img)
		}
	}
	if len(msg.Photo) > 0 {
		part, imgs := describePhoto(bot, msg.Photo)
		parts = append(parts, part)
		images = append(images, imgs...)
	}
	if msg.Audio != nil {
		parts = append(parts, describeAudio(bot, msg.Audio))
	}
	if msg.Voice != nil {
		parts = append(parts, describeVoice(bot, msg.Voice))
	}
	if msg.Video != nil {
		parts = append(parts, describeVideo(bot, msg.Video))
	}
	if msg.VideoNote != nil {
		parts = append(parts, describeVideoNote(bot, msg.VideoNote))
	}
	if msg.Sticker != nil {
		parts = append(parts, fmt.Sprintf(
			"Sticker received: set %s, emoji %s",
			msg.Sticker.SetName, msg.Sticker.Emoji,
		))
	}
	if msg.Animation != nil {
		part, img := describeAnimation(bot, msg.Animation)
		parts = append(parts, part)
		if img.DataURL != "" {
			images = append(images, img)
		}
	}

	return parts, images
}

func describeDocument(bot *tgbotapi.BotAPI, doc *tgbotapi.Document) (string, chat.Image) {
	part := fmt.Sprintf(
		"Document: %s (%d bytes, mime %s).",
		doc.FileName, doc.FileSize, doc.MimeType,
	)
	if strings.HasPrefix(doc.MimeType, "image/") {
		dataURL, err := fetchDataURL(bot, doc.FileID, doc.MimeType)
		if err != nil {
			log.Printf("could not fetch image document: %v", err)
			return part, chat.Image{}
		}
		return part, chat.Image{DataURL: dataURL}
	}
	return part, chat.Image{}
}

func describePhoto(bot *tgbotapi.BotAPI, photos []tgbotapi.PhotoSize) (string, []chat.Image) {
	best := photos[len(photos)-1]
	part := fmt.Sprintf(
		"Photo: resolution %dx%d (%d bytes).",
		best.Width, best.Height, best.FileSize,
	)
	dataURL, err := fetchDataURL(bot, best.FileID, "image/jpeg")
	if err != nil {
		log.Printf("could not fetch photo: %v", err)
		return part, nil
	}
	return part, []chat.Image{{DataURL: dataURL}}
}

func describeAudio(bot *tgbotapi.BotAPI, audio *tgbotapi.Audio) string {
	return fmt.Sprintf(
		"Audio: %s (%d sec, %d bytes, mime %s).",
		audio.Title, audio.Duration, audio.FileSize, audio.MimeType,
	)
}

func describeVoice(bot *tgbotapi.BotAPI, voice *tgbotapi.Voice) string {
	return fmt.Sprintf(
		"Voice message: duration %d sec (%d bytes, mime %s).",
		voice.Duration, voice.FileSize, voice.MimeType,
	)
}

func describeVideo(bot *tgbotapi.BotAPI, video *tgbotapi.Video) string {
	return fmt.Sprintf(
		"Video: resolution %dx%d (%d sec, %d bytes, mime %s).",
		video.Width, video.Height, video.Duration,
		video.FileSize, video.MimeType,
	)
}

func describeVideoNote(bot *tgbotapi.BotAPI, note *tgbotapi.VideoNote) string {
	return fmt.Sprintf(
		"Video note: resolution %dx%d (%d sec, %d bytes).",
		note.Length, note.Length, note.Duration, note.FileSize,
	)
}

func describeAnimation(bot *tgbotapi.BotAPI, animation *tgbotapi.Animation) (string, chat.Image) {
	name := animation.FileName
	if name == "" {
		name = filepath.Base(animation.FileID)
	}
	part := fmt.Sprintf(
		"Animation: %s (%d bytes, mime %s).",
		name, animation.FileSize, animation.MimeType,
	)
	if strings.HasPrefix(animation.MimeType, "image/") {
		dataURL, err := fetchDataURL(bot, animation.FileID, animation.MimeType)
		if err != nil {
			log.Printf("could not fetch animation image: %v", err)
			return part, chat.Image{}
		}
		return part, chat.Image{DataURL: dataURL}
	}
	return part, chat.Image{}
}

func fetchDataURL(bot *tgbotapi.BotAPI, fileID, fallbackMime string) (string, error) {
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", bot.Token, file.FilePath)

	resp, err := http.Get(url) // #nosec G107
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	mimeType := resp.Header.Get("Content-Type")
	// prefer declared image mime; otherwise try fallback and extension
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		if strings.HasPrefix(strings.ToLower(fallbackMime), "image/") {
			mimeType = fallbackMime
		} else {
			extMime := mime.TypeByExtension(filepath.Ext(file.FilePath))
			if strings.HasPrefix(strings.ToLower(extMime), "image/") {
				mimeType = extMime
			}
		}
	}
	if mimeType == "" {
		mimeType = fallbackMime
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(file.FilePath))
	}
	if mimeType == "" {
		return "", fmt.Errorf("non-image mime: unknown")
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return "", fmt.Errorf("non-image mime: %s", mimeType)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}
