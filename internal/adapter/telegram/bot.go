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
	"chatgpt-telegram-bot/internal/usecase/transcribe"
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
	api          *tgbotapi.BotAPI
	cfg          config.Config
	chat         *chat.Service
	tts          *tts.Service
	img          *imagegen.Service
	transcribe   *transcribe.Service
	albums       *albumCollector
	imageLimiter *rateLimiter
}

func NewBot(cfg config.Config, chatSvc *chat.Service, ttsSvc *tts.Service, imgSvc *imagegen.Service, transcribeSvc *transcribe.Service) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:          api,
		cfg:          cfg,
		chat:         chatSvc,
		tts:          ttsSvc,
		img:          imgSvc,
		transcribe:   transcribeSvc,
		imageLimiter: newRateLimiter(cfg.ImageLimitPerHour, time.Hour),
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.registerCommands()
	b.albums = newAlbumCollector(func(msgs []*tgbotapi.Message) {
		primary, rest := splitAlbum(msgs)
		b.handleMessage(ctx, primary, rest)
	})

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)
	defer b.api.StopReceivingUpdates()

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
			if msg.MediaGroupID != "" {
				b.albums.add(msg)
				continue
			}
			go b.handleMessage(ctx, msg, nil)
		}
	}
}

// handleMessage processes msg; album holds the other messages of the same
// media group when msg is part of one.
func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message, album []*tgbotapi.Message) {
	// a photo sent with "/img ..." carries the command in its caption
	commandText := msg.Text
	if commandText == "" {
		commandText = msg.Caption
	}

	if isCommandForOtherBot(commandText, b.api.Self.UserName) {
		return
	}
	if !msg.Chat.IsPrivate() && b.cfg.GroupMentionOnly && !b.isAddressed(msg, commandText) {
		return
	}

	if !isAllowedUser(msg.From.ID, msg.Chat.ID, b.cfg) {
		// in groups stay silent, otherwise the bot spams every message
		// of a chat it was added to without permission
		if !msg.Chat.IsPrivate() {
			return
		}
		b.sendText(msg.Chat.ID, msg.MessageID, "access denied")
		return
	}

	if ok, _ := extractCommandText(commandText, "help"); ok {
		b.sendText(msg.Chat.ID, msg.MessageID, helpText)
		return
	}
	if ok, _ := extractCommandText(commandText, "start"); ok {
		b.sendText(msg.Chat.ID, msg.MessageID, helpText)
		return
	}
	if ok, _ := extractCommandText(commandText, "reset"); ok {
		b.chat.Reset(msg.Chat.ID)
		b.sendText(msg.Chat.ID, msg.MessageID, "conversation history cleared")
		return
	}
	if ok, text := extractCommandText(commandText, "tts"); ok {
		b.handleTTS(ctx, msg, text)
		return
	}
	ok, text := extractCommandText(commandText, "img")
	if !ok {
		// /image is kept as an alias from the old command menu
		ok, text = extractCommandText(commandText, "image")
	}
	if ok {
		b.handleImage(ctx, msg, album, text)
		return
	}
	if name, ok := unknownCommand(commandText, b.api.Self.UserName); ok {
		if name != "" {
			b.sendText(msg.Chat.ID, msg.MessageID, "unknown command /"+name+"\n\n"+helpText)
		}
		return
	}

	b.handleChat(ctx, msg, album)
}

func (b *Bot) handleTTS(ctx context.Context, msg *tgbotapi.Message, text string) {
	if strings.TrimSpace(text) == "" {
		b.sendText(msg.Chat.ID, msg.MessageID, "usage: /tts <text>")
		return
	}

	stop := b.keepAction(ctx, msg.Chat.ID, tgbotapi.ChatRecordVoice)
	audio, err := b.tts.Synthesize(ctx, text)
	stop()
	if err != nil {
		switch {
		case errors.Is(err, tts.ErrEmptyText):
			b.sendText(msg.Chat.ID, msg.MessageID, "i need some text to synthesize")
		case errors.Is(err, tts.ErrTextTooLong):
			b.sendText(msg.Chat.ID, msg.MessageID, fmt.Sprintf("text is too long, the limit is %d characters", tts.MaxTextLength))
		default:
			log.Printf("tts request failed: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "failed to generate audio, try again later")
		}
		return
	}

	if err := b.sendVoice(msg.Chat.ID, msg.MessageID, audio); err != nil {
		log.Printf("failed to send voice: %v", err)
		b.sendText(msg.Chat.ID, msg.MessageID, "could not send voice message")
	}
}

func (b *Bot) handleImage(ctx context.Context, msg *tgbotapi.Message, album []*tgbotapi.Message, prompt string) {
	if strings.TrimSpace(prompt) == "" {
		b.sendText(msg.Chat.ID, msg.MessageID, "usage: /img <prompt>\nattach a photo or reply to one to edit it")
		return
	}
	if !b.isAdmin(msg.From.ID) && !b.imageLimiter.allow(msg.From.ID) {
		b.sendText(msg.Chat.ID, msg.MessageID, fmt.Sprintf("image limit reached (%d per hour), try again later", b.cfg.ImageLimitPerHour))
		return
	}

	stop := b.keepAction(ctx, msg.Chat.ID, tgbotapi.ChatUploadPhoto)
	defer stop()

	inputs, err := b.collectInputImages(msg, album)
	if err != nil {
		log.Printf("could not load input image: %v", err)
		b.sendText(msg.Chat.ID, msg.MessageID, "could not load the source image, only png, jpeg and webp are supported")
		return
	}
	imageResp, err := b.img.Generate(ctx, prompt, inputs...)
	if err != nil {
		switch {
		case errors.Is(err, imagegen.ErrEmptyPrompt):
			b.sendText(msg.Chat.ID, msg.MessageID, "i need a prompt to generate an image")
		case errors.Is(err, imagegen.ErrModerationBlocked):
			log.Printf("image generation failed: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "openai's safety filter rejected this request, try a different prompt or image")
		default:
			log.Printf("image generation failed: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "failed to generate image, try again later")
		}
		return
	}

	if err := b.sendImage(msg.Chat.ID, msg.MessageID, imageResp); err != nil {
		log.Printf("failed to send image: %v", err)
		b.sendText(msg.Chat.ID, msg.MessageID, "could not send image")
	}
}

func (b *Bot) handleChat(ctx context.Context, msg *tgbotapi.Message, album []*tgbotapi.Message) {
	userInput, respondAsFile := b.buildUserInput(ctx, msg, album)

	action := tgbotapi.ChatTyping
	if respondAsFile {
		action = tgbotapi.ChatUploadDocument
	}
	stop := b.keepAction(ctx, msg.Chat.ID, action)
	resp, err := b.chat.HandleMessage(ctx, msg.Chat.ID, userInput)
	stop()
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrEmptyMessage):
			b.sendText(msg.Chat.ID, msg.MessageID, "i need some content to work with")
		case errors.Is(err, chat.ErrEmptyResponse):
			log.Printf("openai request failed: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "the model returned an empty answer, try rephrasing or raise MAX_TOKENS")
		default:
			log.Printf("openai request failed: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "failed to reach openai, try again later")
		}
		return
	}

	if respondAsFile || shouldSendAsFile(resp) {
		if err := b.sendAsFile(msg.Chat.ID, msg.MessageID, resp); err != nil {
			log.Printf("failed to send file: %v", err)
			b.sendText(msg.Chat.ID, msg.MessageID, "could not send file, here is the text")
			b.sendText(msg.Chat.ID, msg.MessageID, resp)
		}
		return
	}

	b.sendFormatted(msg.Chat.ID, msg.MessageID, resp)
}

// collectInputImages loads the images to edit: those attached to the
// command message or its album and the one in the message it replies to.
func (b *Bot) collectInputImages(msg *tgbotapi.Message, album []*tgbotapi.Message) ([]imagegen.InputImage, error) {
	sources := append([]*tgbotapi.Message{msg}, album...)
	sources = append(sources, msg.ReplyToMessage)

	var inputs []imagegen.InputImage
	for _, m := range sources {
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

func (b *Bot) isAdmin(userID int64) bool {
	for _, id := range b.cfg.AdminUserIDs {
		if id == userID {
			return true
		}
	}
	return false
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

// sendFormatted renders model Markdown as Telegram HTML and falls back to
// plain text when Telegram rejects the markup.
func (b *Bot) sendFormatted(chatID int64, replyTo int, text string) {
	msg := tgbotapi.NewMessage(chatID, markdownToHTML(text))
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyToMessageID = replyTo
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("failed to send formatted reply, falling back to plain text: %v", err)
		b.sendText(chatID, replyTo, text)
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

// maxInlineReply keeps a formatted reply under Telegram's 4096 character
// limit with room for UTF-16 surrogate pairs; longer answers go as a file.
const maxInlineReply = 3500

func shouldSendAsFile(text string) bool {
	return len([]rune(text)) > maxInlineReply
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

// maxQuotedRunes caps how much of a replied-to message goes into the prompt.
const maxQuotedRunes = 3000

// buildUserInput assembles the prompt from the message, its album, the
// message it replies to and, in groups, the sender name.
func (b *Bot) buildUserInput(ctx context.Context, msg *tgbotapi.Message, album []*tgbotapi.Message) (chat.Input, bool) {
	text := msg.Text
	respondAsFile, fileText := extractCommandText(text, "file")
	if respondAsFile {
		text = fileText
	}
	group := !msg.Chat.IsPrivate()

	parts := make([]string, 0, 8)
	if group {
		parts = append(parts, "["+displayName(msg.From)+"]:")
	}
	if text = b.stripMention(text); text != "" {
		parts = append(parts, text)
	}
	if caption := b.stripMention(msg.Caption); caption != "" {
		parts = append(parts, caption)
	}

	var images []chat.Image
	for _, m := range append([]*tgbotapi.Message{msg}, album...) {
		attachmentParts, imgs := b.describeAttachments(ctx, m)
		parts = append(parts, attachmentParts...)
		images = append(images, imgs...)
	}

	if r := msg.ReplyToMessage; r != nil && hasUserContent(r) {
		quoted := make([]string, 0, 4)
		if t := strings.TrimSpace(r.Text + "\n" + r.Caption); t != "" {
			quoted = append(quoted, truncateRunes(t, maxQuotedRunes))
		}
		attachmentParts, imgs := b.describeAttachments(ctx, r)
		quoted = append(quoted, attachmentParts...)
		images = append(images, imgs...)
		parts = append(parts, "\nIn reply to a message from "+displayName(r.From)+":\n"+strings.Join(quoted, "\n"))
	}

	return chat.Input{
		Text:   strings.TrimSpace(strings.Join(parts, "\n")),
		Images: images,
	}, respondAsFile
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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
