package telegram

import (
	"context"
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
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"chatgpt-telegram-bot/internal/usecase/chat"
)

const (
	// maxDownloadBytes is the Telegram Bot API download limit; it also
	// guards against pulling huge files into memory.
	maxDownloadBytes = 20 << 20
	// maxTextFileBytes caps text documents inlined into the prompt.
	maxTextFileBytes = 100 << 10
)

var fileClient = &http.Client{Timeout: 60 * time.Second}

// describeAttachments turns the attachments of msg into prompt text and
// images: photos are passed as images, audio and video are transcribed and
// text documents are inlined.
func (b *Bot) describeAttachments(ctx context.Context, msg *tgbotapi.Message) ([]string, []chat.Image) {
	parts := make([]string, 0, 4)
	images := make([]chat.Image, 0, 1)

	if msg.Document != nil {
		part, img := b.describeDocument(msg.Document)
		parts = append(parts, part)
		if img.DataURL != "" {
			images = append(images, img)
		}
	}
	if len(msg.Photo) > 0 {
		best := msg.Photo[len(msg.Photo)-1]
		parts = append(parts, fmt.Sprintf("Photo: resolution %dx%d.", best.Width, best.Height))
		if dataURL, err := fetchDataURL(b.api, best.FileID, "image/jpeg"); err != nil {
			log.Printf("could not fetch photo: %v", err)
		} else {
			images = append(images, chat.Image{DataURL: dataURL})
		}
	}
	if msg.Voice != nil {
		label := fmt.Sprintf("Voice message (%d sec)", msg.Voice.Duration)
		parts = append(parts, b.transcribeMedia(ctx, label, msg.Voice.FileID, "voice.ogg", msg.Voice.FileSize))
	}
	if msg.VideoNote != nil {
		label := fmt.Sprintf("Round video message (%d sec)", msg.VideoNote.Duration)
		parts = append(parts, b.transcribeMedia(ctx, label, msg.VideoNote.FileID, "video.mp4", msg.VideoNote.FileSize))
	}
	if msg.Audio != nil {
		label := fmt.Sprintf("Audio %q (%d sec)", strings.TrimSpace(msg.Audio.Performer+" "+msg.Audio.Title), msg.Audio.Duration)
		parts = append(parts, b.transcribeMedia(ctx, label, msg.Audio.FileID, mediaFilename("audio", msg.Audio.MimeType), msg.Audio.FileSize))
	}
	if msg.Video != nil {
		label := fmt.Sprintf("Video %dx%d (%d sec)", msg.Video.Width, msg.Video.Height, msg.Video.Duration)
		parts = append(parts, b.transcribeMedia(ctx, label, msg.Video.FileID, mediaFilename("video", msg.Video.MimeType), msg.Video.FileSize))
	}
	if msg.Sticker != nil {
		parts = append(parts, fmt.Sprintf("Sticker: emoji %s (set %s).", msg.Sticker.Emoji, msg.Sticker.SetName))
	}
	if msg.Animation != nil {
		parts = append(parts, fmt.Sprintf("GIF animation (%d sec), its content is not visible to you.", msg.Animation.Duration))
	}

	return parts, images
}

func (b *Bot) describeDocument(doc *tgbotapi.Document) (string, chat.Image) {
	part := fmt.Sprintf("Document: %s (%d bytes, mime %s).", doc.FileName, doc.FileSize, doc.MimeType)

	if strings.HasPrefix(doc.MimeType, "image/") {
		dataURL, err := fetchDataURL(b.api, doc.FileID, doc.MimeType)
		if err != nil {
			log.Printf("could not fetch image document: %v", err)
			return part, chat.Image{}
		}
		return part, chat.Image{DataURL: dataURL}
	}

	if !isTextDocument(doc.FileName, doc.MimeType) {
		return part, chat.Image{}
	}
	if doc.FileSize > maxTextFileBytes {
		return part + " It is too large to read.", chat.Image{}
	}
	data, _, err := fetchFile(b.api, doc.FileID, maxTextFileBytes)
	if err != nil {
		log.Printf("could not fetch text document: %v", err)
		return part, chat.Image{}
	}
	if !utf8.Valid(data) {
		return part + " It is not valid UTF-8 text.", chat.Image{}
	}
	return fmt.Sprintf("Document %s content:\n```\n%s\n```", doc.FileName, data), chat.Image{}
}

// transcribeMedia downloads audio or video and returns its transcript,
// falling back to a plain description when that is not possible.
func (b *Bot) transcribeMedia(ctx context.Context, label, fileID, filename string, size int) string {
	fallback := label + ", could not be transcribed."
	if b.transcribe == nil || filename == "" {
		return label + ", its format cannot be transcribed."
	}
	if size > maxDownloadBytes {
		return label + ", too large to transcribe."
	}

	data, _, err := fetchFile(b.api, fileID, maxDownloadBytes)
	if err != nil {
		log.Printf("could not fetch media: %v", err)
		return fallback
	}
	text, err := b.transcribe.Transcribe(ctx, filename, data)
	if err != nil {
		log.Printf("transcription failed: %v", err)
		return fallback
	}
	if text == "" {
		return label + ", no speech detected."
	}
	return label + " transcript:\n" + text
}

// transcribableTypes maps mime types to extensions the transcription
// endpoint accepts.
var transcribableTypes = map[string]string{
	"audio/mpeg":  "mp3",
	"audio/mp3":   "mp3",
	"audio/ogg":   "ogg",
	"audio/opus":  "ogg",
	"audio/mp4":   "m4a",
	"audio/m4a":   "m4a",
	"audio/x-m4a": "m4a",
	"audio/aac":   "m4a",
	"audio/wav":   "wav",
	"audio/x-wav": "wav",
	"audio/flac":  "flac",
	"audio/webm":  "webm",
	"video/mp4":   "mp4",
	"video/webm":  "webm",
}

func mediaFilename(base, mimeType string) string {
	ext, ok := transcribableTypes[strings.ToLower(mimeType)]
	if !ok {
		return ""
	}
	return base + "." + ext
}

var textExtensions = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".tsv": true, ".log": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".xml": true,
	".html": true, ".css": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".go": true, ".py": true, ".rb": true, ".php": true, ".java": true, ".kt": true,
	".swift": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cs": true,
	".rs": true, ".sh": true, ".sql": true, ".proto": true, ".tex": true,
}

func isTextDocument(name, mimeType string) bool {
	mimeType = strings.ToLower(mimeType)
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/xml", "application/x-yaml", "application/javascript", "application/x-sh", "application/sql":
		return true
	}
	return textExtensions[strings.ToLower(filepath.Ext(name))]
}

// fetchFile downloads a Telegram file of at most maxBytes and returns its
// data and the server-declared content type.
func fetchFile(bot *tgbotapi.BotAPI, fileID string, maxBytes int) ([]byte, string, error) {
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

	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxBytes {
		return nil, "", fmt.Errorf("download %s: file larger than %d bytes", file.FilePath, maxBytes)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		// Telegram often answers application/octet-stream, the extension of
		// the stored file is a better hint
		if extMime := mime.TypeByExtension(filepath.Ext(file.FilePath)); extMime != "" {
			contentType = extMime
		}
	}
	return data, contentType, nil
}

func fetchImage(bot *tgbotapi.BotAPI, fileID, fallbackMime string) ([]byte, string, error) {
	data, mimeType, err := fetchFile(bot, fileID, maxDownloadBytes)
	if err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		mimeType = fallbackMime
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, "", fmt.Errorf("non-image mime: %q", mimeType)
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
