package config

import (
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
	ImageModel          string
	ImageSize           string
	ImageQuality        string
	ImageFormat         string
	ImageBackground     string
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
		Model:               getenvDefault("OPENAI_MODEL", "gpt-6-luna"),
		TTSModel:            getenvDefault("OPENAI_TTS_MODEL", "gpt-4o-mini-tts"),
		TTSVoice:            getenvDefault("OPENAI_TTS_VOICE", "alloy"),
		TTSFormat:           getenvDefault("OPENAI_TTS_FORMAT", "opus"),
		ImageModel:          getenvDefault("OPENAI_IMAGE_MODEL", "gpt-image-2.5-flare"),
		ImageSize:           getenvDefault("OPENAI_IMAGE_SIZE", "auto"),
		ImageQuality:        getenvDefault("OPENAI_IMAGE_QUALITY", "auto"),
		ImageFormat:         getenvDefault("OPENAI_IMAGE_FORMAT", "png"),
		ImageBackground:     getenvDefault("OPENAI_IMAGE_BACKGROUND", ""),
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
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	for key, val := range parseDotEnv(string(data)) {
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return nil
}

// parseDotEnv parses KEY=VALUE lines. A value wrapped in single or double
// quotes may span multiple lines, e.g. a long ASSISTANT_PROMPT.
func parseDotEnv(content string) map[string]string {
	res := make(map[string]string)
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := parseEnvLine(line)
		if !ok {
			continue
		}

		if quote, open := openQuote(val); open {
			buf := []string{val[1:]}
			closed := false
			for i+1 < len(lines) {
				i++
				next := strings.TrimRight(lines[i], " \t")
				if strings.HasSuffix(next, quote) {
					buf = append(buf, strings.TrimSuffix(next, quote))
					closed = true
					break
				}
				buf = append(buf, next)
			}
			if !closed {
				log.Printf("unterminated quoted value for %s in .env", key)
			}
			res[key] = strings.Join(buf, "\n")
			continue
		}

		res[key] = strings.Trim(val, `"'`)
	}
	return res
}

// openQuote reports whether val starts with a quote that is not closed on
// the same line.
func openQuote(val string) (string, bool) {
	if val == "" {
		return "", false
	}
	q := val[:1]
	if q != `"` && q != "'" {
		return "", false
	}
	if len(val) >= 2 && strings.HasSuffix(val, q) {
		return "", false
	}
	return q, true
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
	if key == "" {
		return "", "", false
	}
	return key, val, true
}
