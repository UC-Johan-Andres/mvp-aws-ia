package config

import (
	"os"
	"time"
)

const (
	MaxServices  = 2
	Port         = ":8090"
	CookieName   = "bolt_session"
	CookieDomain = ".soylideria.com"
	CookieTTL    = 24 * time.Hour
)

var (
	AuthUser      = getEnv("AUTH_USER", "admin")
	AuthPassword  = getEnv("AUTH_PASSWORD", "")
	SessionSecret = getEnv("SESSION_SECRET", "change_me")
	AgentSocket   = getEnv("AGENT_SOCKET", "/var/run/docker-agent.sock")
)

var HostToService = map[string]string{
	"marimo.soylideria.com":       "marimo",
	"n8ntest.soylideria.com":      "n8n",
	"bolttest.soylideria.com":     "bolt",
	"chatwoottest.soylideria.com": "chatwoot",
	"chat.soylideria.com":         "librechat",
}

var Companions = map[string][]string{
	"chatwoot": {"chatwoot_sidekiq"},
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
