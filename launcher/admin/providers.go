package admin

import (
	"fmt"
	"strings"
)

// IDs de proveedor estables (extensible: añadir gemini, anthropic, etc.).
const (
	ProviderOpenAI = "openai"
	ProviderGoogle = "google" // Gemini (API compatible OpenAI en LibreChat)
)

// ProviderDefinition describe un proveedor de IA configurable por empresa.
type ProviderDefinition struct {
	ID                 string // clave en JSON, ej. "openai"
	DisplayName        string // UI
	LibreChatKeyName   string // nombre en colección Mongo "keys" de LibreChat; vacío = no sincronizar
	N8NCredentialType  string // tipo en n8n (p. ej. openAiApi); vacío = no sincronizar n8n
}

// RegisteredProviders lista conocida; mañana basta con añadir entradas aquí.
func RegisteredProviders() []ProviderDefinition {
	return []ProviderDefinition{
		{ID: ProviderOpenAI, DisplayName: "OpenAI (ChatGPT)", LibreChatKeyName: "OpenAI", N8NCredentialType: "openAiApi"},
		{ID: ProviderGoogle, DisplayName: "Google Gemini", LibreChatKeyName: "Google Gemini", N8NCredentialType: "googlePalmApi"},
	}
}

// N8NCredentialData construye el objeto data para crear/actualizar credencial en n8n.
func N8NCredentialData(providerID, apiKey string) map[string]any {
	switch providerID {
	case ProviderOpenAI:
		return map[string]any{"apiKey": apiKey}
	case ProviderGoogle:
		return map[string]any{
			"apiKey": apiKey,
			"host":   "https://generativelanguage.googleapis.com",
		}
	default:
		return map[string]any{"apiKey": apiKey}
	}
}

// ProviderCredential almacena secretos por proveedor (extensible con más campos).
type ProviderCredential struct {
	APIKey string `json:"apiKey,omitempty"`
}

// IsKnownProviderID indica si el id es reconocido en RegisteredProviders.
func IsKnownProviderID(id string) bool {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, p := range RegisteredProviders() {
		if strings.EqualFold(p.ID, id) {
			return true
		}
	}
	return false
}

// NormalizeProviderID devuelve el id canónico (minúsculas) o error.
func NormalizeProviderID(id string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(id))
	if s == "" {
		return "", fmt.Errorf("proveedor vacío")
	}
	for _, p := range RegisteredProviders() {
		if strings.EqualFold(p.ID, id) {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("proveedor desconocido: %q (use openai, google)", id)
}

const maxAPIKeyLen = 2048

// SanitizeProviderCredential valida y recorta; devuelve copia segura para guardar.
func SanitizeProviderCredential(c ProviderCredential) (ProviderCredential, error) {
	k := strings.TrimSpace(c.APIKey)
	if k == "" {
		return ProviderCredential{}, nil
	}
	if len(k) > maxAPIKeyLen {
		return ProviderCredential{}, fmt.Errorf("apiKey demasiado largo")
	}
	return ProviderCredential{APIKey: k}, nil
}

// MaskAPIKey para listados (nunca exponer la clave completa).
func MaskAPIKey(key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "••••"
	}
	return "…" + k[len(k)-4:]
}

// NormalizeCredentialsMapInput valida ids de proveedor y recorta valores; omite entradas sin apiKey.
// Devuelve error si alguna clave del mapa no es un proveedor conocido.
func NormalizeCredentialsMapInput(in map[string]ProviderCredential) (map[string]ProviderCredential, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]ProviderCredential)
	for id, c := range in {
		nid, err := NormalizeProviderID(id)
		if err != nil {
			return nil, err
		}
		sc, err := SanitizeProviderCredential(c)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", nid, err)
		}
		if sc.APIKey == "" {
			continue
		}
		out[nid] = sc
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// MergeCredentialsMaps combina patch sobre base (patch sobrescribe por proveedor).
func MergeCredentialsMaps(base, patch map[string]ProviderCredential) map[string]ProviderCredential {
	out := make(map[string]ProviderCredential)
	for k, v := range base {
		out[k] = v
	}
	for k, v := range patch {
		if strings.TrimSpace(v.APIKey) == "" {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}
