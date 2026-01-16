package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"chatgpt-telegram-bot/internal/adapter/memory"
	"chatgpt-telegram-bot/internal/adapter/openai"
	"chatgpt-telegram-bot/internal/adapter/telegram"
	"chatgpt-telegram-bot/internal/config"
	"chatgpt-telegram-bot/internal/usecase/chat"
	"chatgpt-telegram-bot/internal/usecase/image"
	"chatgpt-telegram-bot/internal/usecase/tts"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	openAIClient := openai.NewClient(cfg.OpenAIKey)
	store := memory.NewStore()
	chatSvc := chat.NewService(store, openAIClient, cfg)
	ttsSvc := tts.NewService(openAIClient, cfg)
	imgSvc := image.NewService(openAIClient, cfg)

	bot, err := telegram.NewBot(cfg, chatSvc, ttsSvc, imgSvc)
	if err != nil {
		log.Fatalf("failed to init telegram bot: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := bot.Run(ctx); err != nil {
		if ctx.Err() != nil {
			log.Printf("shutdown: %v", err)
			return
		}
		log.Fatalf("bot stopped with error: %v", err)
	}
}
