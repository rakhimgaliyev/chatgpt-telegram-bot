package telegram

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"chatgpt-telegram-bot/internal/usecase/chat"
)

// maxImageBytes guards against pulling huge image documents into memory
// and sending them to OpenAI; Telegram bots can download up to 20 MB.
const maxImageBytes = 20 << 20

var fileClient = &http.Client{Timeout: 60 * time.Second}

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

func fetchImage(bot *tgbotapi.BotAPI, fileID, fallbackMime string) ([]byte, string, error) {
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, "", err
	}
	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", bot.Token, file.FilePath)

	resp, err := fileClient.Get(fileURL) // #nosec G107
	if err != nil {
		// url.Error embeds the full URL, which contains the bot token
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, "", fmt.Errorf("download %s: %w", file.FilePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download %s: status %d", file.FilePath, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxImageBytes {
		return nil, "", fmt.Errorf("download %s: file larger than %d bytes", file.FilePath, maxImageBytes)
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
		return nil, "", fmt.Errorf("non-image mime: unknown")
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, "", fmt.Errorf("non-image mime: %s", mimeType)
	}

	return data, mimeType, nil
}

func fetchDataURL(bot *tgbotapi.BotAPI, fileID, fallbackMime string) (string, error) {
	data, mimeType, err := fetchImage(bot, fileID, fallbackMime)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}

// imageSource returns the file ID and declared mime type of the image in
// msg: the largest photo size or an image document.
func imageSource(msg *tgbotapi.Message) (string, string, bool) {
	if msg == nil {
		return "", "", false
	}
	if len(msg.Photo) > 0 {
		return msg.Photo[len(msg.Photo)-1].FileID, "image/jpeg", true
	}
	if msg.Document != nil && strings.HasPrefix(msg.Document.MimeType, "image/") {
		return msg.Document.FileID, msg.Document.MimeType, true
	}
	return "", "", false
}
