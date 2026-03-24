package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
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

	MongoURI = getEnv("MONGO_URI", "")
	// LibreChatMongoDB nombre de la base en Mongo (colecciones users, conversations, messages).
	// Debe coincidir con el path de MONGO_URI (p. ej. .../LibreChat?authSource=admin).
	LibreChatMongoDB = getEnv("LIBRECHAT_MONGO_DB", "LibreChat")
	N8NInternalURL = getEnv("N8N_INTERNAL_URL", "http://n8n:5678")
	N8NBasicUser   = getEnv("N8N_BASIC_AUTH_USER", "")
	N8NBasicPass   = getEnv("N8N_BASIC_AUTH_PASSWORD", "")
	N8NOwnerEmail  = getEnv("N8N_OWNER_EMAIL", "")
	N8NOwnerPass   = getEnv("N8N_OWNER_PASSWORD", "")
)

// N8NPostgresDSN devuelve la cadena de conexión a la BD de n8n.
// 1) Si N8N_POSTGRES_DSN está definida, se usa (debe coincidir con la contraseña real del rol en PostgreSQL).
// 2) Si no, se construye con DB_POSTGRESDB_* (las mismas variables que lee el contenedor n8n en .env.n8n).
// Nota: psql dentro del contenedor postgres a veces usa socket Unix con trust; el launcher usa TCP y siempre exige contraseña.
func N8NPostgresDSN() string {
	if v := strings.TrimSpace(os.Getenv("N8N_POSTGRES_DSN")); v != "" {
		return v
	}
	pass := strings.TrimSpace(os.Getenv("DB_POSTGRESDB_PASSWORD"))
	if pass == "" {
		return ""
	}
	user := getEnv("DB_POSTGRESDB_USER", "n8n")
	host := getEnv("DB_POSTGRESDB_HOST", "postgres")
	port := getEnv("DB_POSTGRESDB_PORT", "5432")
	dbname := getEnv("DB_POSTGRESDB_DATABASE", "n8n")
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   "/" + strings.TrimPrefix(dbname, "/"),
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

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

// LibreChatMongoDatabase devuelve el nombre de la BD LibreChat en MongoDB.
func LibreChatMongoDatabase() string {
	return LibreChatMongoDB
}
