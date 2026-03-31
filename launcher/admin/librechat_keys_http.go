package admin

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"launcher/config"
)

// mintLibreChatJWT genera un JWT HS256 con el payload mínimo que acepta
// el middleware requireJwtAuth de LibreChat (solo necesita "id").
func mintLibreChatJWT(userIDHex string) (string, error) {
	secret := strings.TrimSpace(config.LibreChatJWTSecret)
	if secret == "" {
		return "", fmt.Errorf("LIBRECHAT_JWT_SECRET no definido")
	}

	header := base64URLEncode([]byte(`{"alg":"HS256","typ":"JWT"}`))

	now := time.Now().Unix()
	payload := map[string]any{
		"id":  userIDHex,
		"iat": now,
		"exp": now + 120, // 2 min — solo para esta llamada interna
	}
	payloadJSON, _ := json.Marshal(payload)
	body := base64URLEncode(payloadJSON)

	unsigned := header + "." + body
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	sig := base64URLEncode(mac.Sum(nil))

	return unsigned + "." + sig, nil
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// putLibreChatUserKey llama PUT /api/keys de LibreChat para que él cifre y almacene la clave.
// Devuelve nil si la operación fue exitosa.
func putLibreChatUserKey(userIDHex, keyName, apiKeyPlaintext string) error {
	baseURL := strings.TrimRight(config.LibreChatInternalURL, "/")
	if baseURL == "" {
		return fmt.Errorf("LIBRECHAT_INTERNAL_URL vacío")
	}

	token, err := mintLibreChatJWT(userIDHex)
	if err != nil {
		return err
	}

	body, _ := json.Marshal(map[string]string{
		"name":  keyName,
		"value": apiKeyPlaintext,
	})

	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/keys", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP PUT /api/keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("PUT /api/keys %q → %d: %s", keyName, resp.StatusCode, string(respBody))
}

// deleteLibreChatUserKeyHTTP llama DELETE /api/keys/:name de LibreChat.
func deleteLibreChatUserKeyHTTP(userIDHex, keyName string) error {
	baseURL := strings.TrimRight(config.LibreChatInternalURL, "/")
	if baseURL == "" {
		return fmt.Errorf("LIBRECHAT_INTERNAL_URL vacío")
	}

	token, err := mintLibreChatJWT(userIDHex)
	if err != nil {
		return err
	}

	encodedName := strings.ReplaceAll(keyName, " ", "%20")
	req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/keys/"+encodedName, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP DELETE /api/keys/%s: %w", keyName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == 404 {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("DELETE /api/keys/%s → %d: %s", keyName, resp.StatusCode, string(respBody))
}

// librechatHTTPKeysAvailable indica si podemos usar la API HTTP de LibreChat para gestionar keys.
func librechatHTTPKeysAvailable() bool {
	return strings.TrimSpace(config.LibreChatJWTSecret) != "" &&
		strings.TrimSpace(config.LibreChatInternalURL) != ""
}

func init() {
	if librechatHTTPKeysAvailable() {
		log.Println("librechat-keys: usando API HTTP de LibreChat (PUT /api/keys) para sincronizar claves")
	} else {
		log.Println("librechat-keys: LIBRECHAT_JWT_SECRET no definido — sync de claves usará cifrado local (CREDS_KEY/CREDS_IV)")
	}
}
