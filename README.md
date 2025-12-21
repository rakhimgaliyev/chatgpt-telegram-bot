# ChatGPT Telegram Bot

Go Telegram bot that proxies users to OpenAI chat completions (non-streaming). Supports multimodal requests (text + images), context window with TTL, and returning replies as a file on demand.

## Features
- Replies to messages via OpenAI ChatCompletion (no streaming).
- Multimodal: photos/image documents are inlined as data URLs for the model.
- Context: keeps up to `CONTEXT_MESSAGE_LIMIT` fresh messages within `CONTEXT_TTL_MINUTES`.
- Access control: admins always allowed; optional allow-list for others.
- `/file <prompt>` returns the answer as `response.txt`.
- Handles attachments (photos, docs, audio/video/voice/sticker/animation) by describing them in the prompt; images are passed to OpenAI.

## Config (.env)
See `.env.example`:
- `OPENAI_API_KEY` (required)
- `TELEGRAM_BOT_TOKEN` (required)
- `OPENAI_MODEL` (default `gpt-5.1`)
- `ADMIN_USER_IDS`
- `ALLOWED_TELEGRAM_USER_IDS`
- `ASSISTANT_PROMPT` (default `You are telegram bot assistant`)
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
