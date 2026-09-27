# ChatGPT Telegram Bot

Go Telegram bot that proxies users to OpenAI chat completions (non-streaming). Supports multimodal requests (text + images), context window with TTL, and returning replies as a file on demand.

## Features
- Replies to messages via OpenAI ChatCompletion (no streaming).
- Multimodal: photos/image documents are inlined as data URLs for the model.
- Context: keeps up to `CONTEXT_MESSAGE_LIMIT` fresh messages within `CONTEXT_TTL_MINUTES`.
- Access control: admins always allowed; optional allow-list for users or chats. Unauthorized groups are ignored silently.
- Service messages (video chat started, member joined, etc.) are ignored.
- `/file <prompt>` returns the answer as `response.md`.
- `/tts <text>` returns synthesized speech as a voice message.
- `/img <prompt>` generates an image and returns it as a photo.
- Handles attachments (photos, docs, audio/video/voice/sticker/animation) by describing them in the prompt; images are passed to OpenAI.

## Config (.env)
See `.env.example`:
- `OPENAI_API_KEY` (required)
- `TELEGRAM_BOT_TOKEN` (required)
- `OPENAI_MODEL` (default `gpt-6-luna`)
- `OPENAI_TTS_MODEL` (default `gpt-4o-mini-tts`)
- `OPENAI_TTS_VOICE` (default `alloy`)
- `OPENAI_TTS_FORMAT` (default `opus`, recommended for voice messages)
- `OPENAI_IMAGE_MODEL` (default `gpt-image-2.5-flare`, used with the Images API)
- `OPENAI_IMAGE_SIZE` (default `auto`)
- `OPENAI_IMAGE_QUALITY` (default `auto`)
- `OPENAI_IMAGE_FORMAT` (default `png`)
- `OPENAI_IMAGE_BACKGROUND` (optional, default empty)
- `ADMIN_USER_IDS`
- `ALLOWED_TELEGRAM_USER_IDS`
- `ALLOWED_TELEGRAM_CHAT_IDS`
- `ASSISTANT_PROMPT` (default `You are telegram bot assistant`; wrap in quotes to span multiple lines)
- `MAX_TOKENS` (max completion tokens, default `4096`)
- `CONTEXT_MESSAGE_LIMIT` (default `20`)
- `CONTEXT_TTL_MINUTES` (default `120`)

Values can be set via environment or `.env`; `.env` is loaded if present.

## Run
```bash
cp .env.example .env   # fill secrets
make run               # or: go run ./cmd/bot
```

Build binary:
```bash
make build   # outputs bin/main
./bin/main
```

## Usage
- Chat normally.
- Send images as photo or image document; the model receives them.
- Prefix with `/file <prompt>` to get reply as file.

## Development
- Format/tests: `gofmt -w ./cmd ./internal && go test ./...`
- Clean binary: `make clean`
