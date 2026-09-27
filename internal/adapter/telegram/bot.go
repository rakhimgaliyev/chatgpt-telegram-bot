package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"chatgpt-telegram-bot/internal/config"
	"chatgpt-telegram-bot/internal/usecase/chat"
	imagegen "chatgpt-telegram-bot/internal/usecase/image"
	"chatgpt-telegram-bot/internal/usecase/tts"
)

// commands is the single source of truth for the Telegram command menu.
var commands = []tgbotapi.BotCommand{
	{Command: "img", Description: "Generate an image, or edit a photo you attach or reply to"},
	{Command: "tts", Description: "Text to speech (e.g. /tts hello)"},
	{Command: "file", Description: "Get the answer as a file (e.g. /file write a report)"},
	{Command: "reset", Description: "Forget the conversation history"},
	{Command: "help", Description: "Show available commands"},
}

const helpText = `Just write a message (images are supported) and I will answer.

/img <prompt> - generate an image; attach a photo or reply to one to edit it
/tts <text> - text to speech
/file <prompt> - get the answer as a file
/reset - forget the conversation history
/help - show this message`

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
	b.registerCommands()

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
			// service messages (video chat started, member joined, pinned, etc.)
			// carry no user content and must not trigger a reply
			if !hasUserContent(msg) {
				continue
			}
			go b.handleMessage(ctx, msg)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if !isAllowedUser(msg.From.ID, msg.Chat.ID, b.cfg) {
		// in groups stay silent, otherwise the bot spams every message
		// of a chat it was added to without permission
		if !msg.Chat.IsPrivate() {
			return
		}
		deny := tgbotapi.NewMessage(msg.Chat.ID, "access denied")
		deny.ReplyToMessageID = msg.MessageID
		if _, err := b.api.Send(deny); err != nil {
			log.Printf("failed to send deny message: %v", err)
		}
		return
	}

	if ok, _ := extractCommandText(msg.Text, "help"); ok {
		b.sendText(msg.Chat.ID, msg.MessageID, helpText)
		return
	}
	if ok, _ := extractCommandText(msg.Text, "start"); ok {
		b.sendText(msg.Chat.ID, msg.MessageID, helpText)
		return
	}
	if ok, _ := extractCommandText(msg.Text, "reset"); ok {
		b.chat.Reset(msg.Chat.ID)
		b.sendText(msg.Chat.ID, msg.MessageID, "conversation history cleared")
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

	// a photo sent with "/img ..." carries the command in its caption
	commandText := msg.Text
	if commandText == "" {
		commandText = msg.Caption
	}
	ok, text := extractCommandText(commandText, "img")
	if !ok {
		// /image is kept as an alias from the old command menu
		ok, text = extractCommandText(commandText, "image")
	}
	if ok {
		if strings.TrimSpace(text) == "" {
			b.sendText(msg.Chat.ID, msg.MessageID, "usage: /img <prompt>\nattach a photo or reply to one to edit it")
			return
		}

		b.sendPhotoAction(msg.Chat.ID)
		inputs, err := b.collectInputImages(msg)
		if err != nil {
			log.Printf("could not load input image: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not load the source image, only png, jpeg and webp are supported")
			return
		}
		imageResp, err := b.img.Generate(ctx, text, inputs...)
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

	if name, ok := unknownCommand(msg.Text, b.api.Self.UserName); ok {
		// commands aimed at other bots in a group are none of our business
		if name != "" {
			b.sendText(msg.Chat.ID, msg.MessageID, "unknown command /"+name+"\n\n"+helpText)
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
		if errors.Is(err, chat.ErrEmptyResponse) {
			b.sendText(msg.Chat.ID, msg.MessageID, "the model returned an empty answer, try rephrasing or raise MAX_TOKENS")
			return
		}
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

// collectInputImages loads the images to edit: the one attached to the
// command message and the one in the message it replies to.
func (b *Bot) collectInputImages(msg *tgbotapi.Message) ([]imagegen.InputImage, error) {
	var inputs []imagegen.InputImage
	for _, m := range []*tgbotapi.Message{msg, msg.ReplyToMessage} {
		fileID, mimeType, ok := imageSource(m)
		if !ok {
			continue
		}
		data, detected, err := fetchImage(b.api, fileID, mimeType)
		if err != nil {
			return nil, err
		}
		detected = strings.ToLower(strings.TrimSpace(strings.SplitN(detected, ";", 2)[0]))
		if detected != "image/png" && detected != "image/jpeg" && detected != "image/webp" {
			return nil, fmt.Errorf("unsupported image type %s", detected)
		}
		inputs = append(inputs, imagegen.InputImage{Data: data, MimeType: detected})
	}
	return inputs, nil
}

// registerCommands replaces the command menu shown in Telegram clients,
// overriding menus left by older versions of the bot for every scope.
func (b *Bot) registerCommands() {
	scopes := []tgbotapi.BotCommandScope{
		tgbotapi.NewBotCommandScopeDefault(),
		tgbotapi.NewBotCommandScopeAllPrivateChats(),
		tgbotapi.NewBotCommandScopeAllGroupChats(),
	}
	for _, scope := range scopes {
		if _, err := b.api.Request(tgbotapi.NewSetMyCommandsWithScope(scope, commands...)); err != nil {
			log.Printf("failed to set commands for scope %s: %v", scope.Type, err)
		}
	}
	// admins get the group menu unless a dedicated one is set
	if _, err := b.api.Request(tgbotapi.NewDeleteMyCommandsWithScope(tgbotapi.NewBotCommandScopeAllChatAdministrators())); err != nil {
		log.Printf("failed to delete admin commands: %v", err)
	}
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
	text = strings.TrimSpace(text)
	if text == "" {
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

func hasUserContent(msg *tgbotapi.Message) bool {
	return strings.TrimSpace(msg.Text) != "" ||
		strings.TrimSpace(msg.Caption) != "" ||
		len(msg.Photo) > 0 ||
		msg.Document != nil ||
		msg.Audio != nil ||
		msg.Voice != nil ||
		msg.Video != nil ||
		msg.VideoNote != nil ||
		msg.Sticker != nil ||
		msg.Animation != nil
}

// unknownCommand reports whether text is a command this bot does not
// handle. The returned name is empty when the command is addressed to a
// different bot and must be ignored silently.
func unknownCommand(text, botUsername string) (string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	first := strings.TrimPrefix(strings.Fields(text)[0], "/")
	name, target, _ := strings.Cut(first, "@")
	if name == "" {
		return "", false
	}
	for _, c := range commands {
		if strings.EqualFold(name, c.Command) {
			return "", false
		}
	}
	if target != "" && !strings.EqualFold(target, botUsername) {
		return "", true
	}
	return name, true
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
	text := msg.Text
	respondAsFile, fileText := extractCommandText(text, "file")
	if respondAsFile {
		text = fileText
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
