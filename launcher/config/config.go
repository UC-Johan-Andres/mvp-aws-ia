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

	MongoURI       = getEnv("MONGO_URI", "")
	N8NInternalURL = getEnv("N8N_INTERNAL_URL", "http://n8n:5678")
	N8NBasicUser   = getEnv("N8N_BASIC_AUTH_USER", "")
	N8NBasicPass   = getEnv("N8N_BASIC_AUTH_PASSWORD", "")
	N8NOwnerEmail  = getEnv("N8N_OWNER_EMAIL", "")
	N8NOwnerPass   = getEnv("N8N_OWNER_PASSWORD", "")
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
