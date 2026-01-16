package telegram

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"chatgpt-telegram-bot/internal/config"
	"chatgpt-telegram-bot/internal/usecase/chat"
	imagegen "chatgpt-telegram-bot/internal/usecase/image"
	"chatgpt-telegram-bot/internal/usecase/tts"
)

type Bot struct {
	api  *tgbotapi.BotAPI
	cfg  config.Config
	chat *chat.Service
	tts  *tts.Service
	img  *imagegen.Service
	now  func() time.Time
}

func NewBot(cfg config.Config, chatSvc *chat.Service, ttsSvc *tts.Service, imgSvc *imagegen.Service) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:  api,
		cfg:  cfg,
		chat: chatSvc,
		tts:  ttsSvc,
		img:  imgSvc,
		now:  time.Now,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			msg := update.Message
			if msg.From == nil {
				continue
			}
			go b.handleMessage(ctx, msg)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if !isAllowedUser(msg.From.ID, msg.Chat.ID, b.cfg) {
		deny := tgbotapi.NewMessage(msg.Chat.ID, "access denied")
		deny.ReplyToMessageID = msg.MessageID
		if _, err := b.api.Send(deny); err != nil {
			log.Printf("failed to send deny message: %v", err)
		}
		return
	}

	if ok, text := extractCommandText(msg.Text, "tts"); ok {
		if strings.TrimSpace(text) == "" {
			b.sendText(msg.Chat.ID, msg.MessageID, "usage: /tts <text>")
			return
		}

		b.sendVoiceAction(msg.Chat.ID)
		audio, err := b.tts.Synthesize(ctx, text)
		if err != nil {
			if errors.Is(err, tts.ErrEmptyText) {
				b.sendText(msg.Chat.ID, msg.MessageID, "i need some text to synthesize")
				return
			}
			log.Printf("tts request failed: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "failed to generate audio, try again later")
			return
		}

		if err := b.sendVoice(msg.Chat.ID, msg.MessageID, audio); err != nil {
			log.Printf("failed to send voice: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not send voice message")
		}
		return
	}

	if ok, text := extractCommandText(msg.Text, "img"); ok {
		if strings.TrimSpace(text) == "" {
			b.sendText(msg.Chat.ID, msg.MessageID, "usage: /img <prompt>")
			return
		}

		b.sendPhotoAction(msg.Chat.ID)
		imageResp, err := b.img.Generate(ctx, text)
		if err != nil {
			if errors.Is(err, imagegen.ErrEmptyPrompt) {
				b.sendText(msg.Chat.ID, msg.MessageID, "i need a prompt to generate an image")
				return
			}
			log.Printf("image generation failed: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "failed to generate image, try again later")
			return
		}

		if err := b.sendImage(msg.Chat.ID, msg.MessageID, imageResp); err != nil {
			log.Printf("failed to send image: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not send image")
		}
		return
	}

	userInput, respondAsFile := BuildUserInput(b.api, msg)
	b.sendChatAction(msg.Chat.ID, respondAsFile)

	resp, err := b.chat.HandleMessage(ctx, msg.Chat.ID, userInput)
	if err != nil {
		if errors.Is(err, chat.ErrEmptyMessage) {
			b.sendText(msg.Chat.ID, msg.MessageID, "i need some content to work with")
			return
		}
		log.Printf("openai request failed: %v", err)
		b.sendText(msg.Chat.ID, msg.MessageID, "failed to reach openai, try again later")
		return
	}

	if respondAsFile {
		if err := b.sendAsFile(msg.Chat.ID, msg.MessageID, resp); err != nil {
			log.Printf("failed to send file: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not send file, here is the text")
			b.sendText(msg.Chat.ID, msg.MessageID, resp)
		}
		return
	}

	if shouldSendAsFile(resp) {
		if err := b.sendAsFile(msg.Chat.ID, msg.MessageID, resp); err != nil {
			log.Printf("failed to send file: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not send file, here is the text")
			b.sendText(msg.Chat.ID, msg.MessageID, resp)
		}
		return
	}

	b.sendText(msg.Chat.ID, msg.MessageID, resp)
}

func (b *Bot) sendText(chatID int64, replyTo int, text string) {
	const chunkSize = 2048

	chunks := splitText(text, chunkSize)
	for idx, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		if idx == 0 {
			msg.ReplyToMessageID = replyTo
		}
		if _, err := b.api.Send(msg); err != nil {
			log.Printf("failed to send reply: %v", err)
		}
	}
}

func (b *Bot) sendChatAction(chatID int64, asFile bool) {
	action := tgbotapi.ChatTyping
	if asFile {
		action = tgbotapi.ChatUploadDocument
	}
	if _, err := b.api.Request(tgbotapi.NewChatAction(chatID, action)); err != nil {
		log.Printf("failed to send chat action: %v", err)
	}
}

func (b *Bot) sendVoiceAction(chatID int64) {
	if _, err := b.api.Request(tgbotapi.NewChatAction(chatID, tgbotapi.ChatUploadVoice)); err != nil {
		log.Printf("failed to send chat action: %v", err)
	}
}

func (b *Bot) sendPhotoAction(chatID int64) {
	if _, err := b.api.Request(tgbotapi.NewChatAction(chatID, tgbotapi.ChatUploadPhoto)); err != nil {
		log.Printf("failed to send chat action: %v", err)
	}
}

func (b *Bot) sendAsFile(chatID int64, replyTo int, content string) error {
	data := []byte(content)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  "response.md",
		Bytes: data,
	})
	doc.ReplyToMessageID = replyTo

	_, err := b.api.Send(doc)
	return err
}

func (b *Bot) sendVoice(chatID int64, replyTo int, resp tts.Response) error {
	ext := strings.TrimSpace(resp.Format)
	if ext == "" {
		ext = "opus"
	}
	if ext == "opus" {
		ext = "ogg"
	}
	filename := "voice." + ext
	voice := tgbotapi.NewVoice(chatID, tgbotapi.FileBytes{
		Name:  filename,
		Bytes: resp.Data,
	})
	voice.ReplyToMessageID = replyTo
	_, err := b.api.Send(voice)
	return err
}

func (b *Bot) sendImage(chatID int64, replyTo int, resp imagegen.Response) error {
	ext := strings.TrimSpace(resp.Format)
	if ext == "" {
		ext = "png"
	}
	if ext == "jpeg" {
		ext = "jpg"
	}
	filename := "image." + ext
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
		Name:  filename,
		Bytes: resp.Data,
	})
	photo.ReplyToMessageID = replyTo
	_, err := b.api.Send(photo)
	return err
}

func shouldSendAsFile(text string) bool {
	const chunkSize = 2048
	return len([]rune(text)) > chunkSize
}

func extractCommandText(text string, command string) (bool, string) {
	if strings.TrimSpace(text) == "" {
		return false, ""
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false, ""
	}
	first := strings.ToLower(parts[0])
	if !strings.HasPrefix(first, "/") {
		return false, ""
	}
	first = strings.TrimPrefix(first, "/")
	first = strings.SplitN(first, "@", 2)[0]
	if first != strings.ToLower(command) {
		return false, ""
	}
	return true, strings.TrimSpace(text[len(parts[0]):])
}

func isAllowedUser(userID int64, chatID int64, cfg config.Config) bool {
	for _, id := range cfg.AdminUserIDs {
		if id == userID {
			return true
		}
	}

	if len(cfg.AllowedUserIDs) == 0 && len(cfg.AllowedChatIDs) == 0 {
		return true
	}

	for _, id := range cfg.AllowedUserIDs {
		if id == userID {
			return true
		}
	}

	for _, id := range cfg.AllowedChatIDs {
		if id == chatID {
			return true
		}
	}

	return false
}

func BuildUserInput(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) (chat.Input, bool) {
	respondAsFile := false
	text := msg.Text
	if strings.HasPrefix(strings.ToLower(text), "/file") {
		respondAsFile = true
		text = strings.TrimSpace(text[len("/file"):])
	}

	parts := make([]string, 0, 6)
	if text != "" {
		parts = append(parts, text)
	}
	if msg.Caption != "" {
		parts = append(parts, "Caption: "+msg.Caption)
	}

	attachmentParts, images := DescribeAttachments(bot, msg)
	parts = append(parts, attachmentParts...)

	return chat.Input{
		Text:   strings.Join(parts, "\n"),
		Images: images,
	}, respondAsFile
}

func splitText(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		return []string{text}
	}

	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}

	chunks := make([]string, 0, len(runes)/chunkSize+1)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}

	return chunks
}
