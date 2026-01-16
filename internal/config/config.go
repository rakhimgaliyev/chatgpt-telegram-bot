package config

import (
	"bufio"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	OpenAIKey           string
	TelegramToken       string
	Model               string
	AdminUserIDs        []int64
	AllowedUserIDs      []int64
	AllowedChatIDs      []int64
	TTSModel            string
	TTSVoice            string
	TTSFormat           string
	AssistantPrompt     string
	MaxCompletionTokens int
	ContextLimit        int
	ContextTTL          time.Duration
}

func Load(path string) (Config, error) {
	if err := loadDotEnv(path); err != nil {
		log.Printf("could not read .env: %v", err)
	}

	cfg := Config{
		Model:               getenvDefault("OPENAI_MODEL", "gpt-5.1"),
		TTSModel:            getenvDefault("OPENAI_TTS_MODEL", "gpt-4o-mini-tts"),
		TTSVoice:            getenvDefault("OPENAI_TTS_VOICE", "alloy"),
		TTSFormat:           getenvDefault("OPENAI_TTS_FORMAT", "opus"),
		AssistantPrompt:     getenvDefault("ASSISTANT_PROMPT", "You are telegram bot assistant"),
		MaxCompletionTokens: getenvIntDefault("MAX_TOKENS", 4096),
		ContextLimit:        getenvIntDefault("CONTEXT_MESSAGE_LIMIT", 20),
		ContextTTL:          time.Duration(getenvIntDefault("CONTEXT_TTL_MINUTES", 120)) * time.Minute,
	}

	cfg.OpenAIKey = os.Getenv("OPENAI_API_KEY")
	cfg.TelegramToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if cfg.OpenAIKey == "" || cfg.TelegramToken == "" {
		return cfg, errors.New("openai api key and telegram token are required")
	}

	cfg.AdminUserIDs = parseIDs(os.Getenv("ADMIN_USER_IDS"))
	cfg.AllowedUserIDs = parseIDs(os.Getenv("ALLOWED_TELEGRAM_USER_IDS"))
	cfg.AllowedChatIDs = parseIDs(os.Getenv("ALLOWED_TELEGRAM_CHAT_IDS"))

	return cfg, nil
}

func parseIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			log.Printf("skipping user id %q: %v", p, err)
			continue
		}
		ids = append(ids, v)
	}
	return ids
}

func getenvDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func getenvIntDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("invalid int for %s=%q, using default %d", key, v, def)
		return def
	}
	return n
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

func parseEnvLine(line string) (string, string, bool) {
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, `"'`)
	if key == "" {
		return "", "", false
	}
	return key, val, true
}
