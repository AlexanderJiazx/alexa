// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	Addr         string
	DataDir      string
	PublicURL    string
	VaultKeyFile string // the key saved logins are encrypted with

	APIKey          string
	BaseURL         string
	Model           string
	ReasoningEffort string
	ASRModel        string
	ImageModel      string

	ChromePath      string
	BrowserIdleMins int
	BrowserHeadless bool
	ViewportWidth   int
	ViewportHeight  int
	MaxTurns        int
	CompactTokens   int // compact the session when a prompt gets this big
	AllowSignup     bool
	SandboxUIDs     bool
	ShellEnabled    bool
	UIDBase         int
	ResendAPIKey    string
	MailFrom        string
	SecureCookies   bool
	DevCORSOrigin   string
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func Load() *Config {
	key := env("META_MODEL_API_KEY", env("MODEL_API_KEY", ""))
	data, _ := filepath.Abs(env("DATA_DIR", "./data"))
	// Per-user UIDs only make sense when we're root on Linux (inside the container).
	canSandbox := runtime.GOOS == "linux" && os.Geteuid() == 0
	return &Config{
		Addr:            env("ADDR", ":8080"),
		DataDir:         data,
		PublicURL:       strings.TrimRight(env("PUBLIC_URL", ""), "/"),
		VaultKeyFile:    env("VAULT_KEY_FILE", filepath.Join(data, "vault.key")),
		APIKey:          key,
		BaseURL:         strings.TrimRight(env("META_BASE_URL", "https://api.meta.ai/v1"), "/"),
		Model:           env("MODEL", "muse-spark-1.3-contributor"),
		ReasoningEffort: env("REASONING_EFFORT", "medium"),
		ASRModel:        env("ASR_MODEL", "muse-voice-transcribe-1.0"),
		ImageModel:      env("IMAGE_MODEL", "muse-image-1.0"),
		ChromePath:      env("CHROME_PATH", ""),
		BrowserIdleMins: envInt("BROWSER_IDLE_MINUTES", 15),
		BrowserHeadless: envBool("BROWSER_HEADLESS", true),
		ViewportWidth:   envInt("VIEWPORT_WIDTH", 1280),
		ViewportHeight:  envInt("VIEWPORT_HEIGHT", 800),
		MaxTurns:        envInt("MAX_TURNS", 200),
		CompactTokens:   envInt("COMPACT_TOKENS", 120000),
		AllowSignup:     envBool("ALLOW_SIGNUP", false),
		SandboxUIDs:     envBool("SANDBOX_UIDS", canSandbox) && canSandbox,
		ShellEnabled:    envBool("SHELL_ENABLED", true),
		UIDBase:         envInt("UID_BASE", 20000),
		ResendAPIKey:    env("RESEND_API_KEY", ""),
		MailFrom:        env("MAIL_FROM", ""),
		SecureCookies:   envBool("SECURE_COOKIES", false),
		DevCORSOrigin:   env("DEV_CORS_ORIGIN", ""),
	}
}
