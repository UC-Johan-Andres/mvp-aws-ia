package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"launcher/config"
)

// librechatJWTUser coincide con el payload que firma generateToken en LibreChat (user.ts + signPayload).
type librechatJWTUser struct {
	IDHex    string
	Username string
	Email    string
	Provider string
}

func mintLibreChatJWT(u librechatJWTUser) (string, error) {
	secret := config.LibreChatJWTSecretForKeys()
	if secret == "" {
		return "", fmt.Errorf("LIBRECHAT_JWT_SECRET o JWT_SECRET deben coincidir con JWT_SECRET del contenedor librechat")
	}
	prov := strings.TrimSpace(u.Provider)
	if prov == "" {
		prov = "local"
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"id":       u.IDHex,
		"username": u.Username,
		"email":    u.Email,
		"provider": prov,
		"iat":      now.Unix(),
		"exp":      now.Add(15 * time.Minute).Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
}

// putLibreChatUserKey llama PUT /api/keys de LibreChat para que él cifre y almacene la clave.
func putLibreChatUserKey(u librechatJWTUser, keyName, apiKeyPlaintext string) error {
	baseURL := strings.TrimRight(config.LibreChatInternalURL, "/")
	if baseURL == "" {
		return fmt.Errorf("LIBRECHAT_INTERNAL_URL vacío")
	}

	token, err := mintLibreChatJWT(u)
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]string{
		"name":  keyName,
		"value": apiKeyPlaintext,
	})
	if err != nil {
		return err
	}

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
func deleteLibreChatUserKeyHTTP(u librechatJWTUser, keyName string) error {
	baseURL := strings.TrimRight(config.LibreChatInternalURL, "/")
	if baseURL == "" {
		return fmt.Errorf("LIBRECHAT_INTERNAL_URL vacío")
	}

	token, err := mintLibreChatJWT(u)
	if err != nil {
		return err
	}

	encodedName := url.PathEscape(keyName)
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

	if resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("DELETE /api/keys/%s → %d: %s", keyName, resp.StatusCode, string(respBody))
}

// librechatHTTPKeysAvailable indica si podemos usar la API HTTP de LibreChat para gestionar keys.
func librechatHTTPKeysAvailable() bool {
	return config.LibreChatJWTSecretForKeys() != "" &&
		strings.TrimSpace(config.LibreChatInternalURL) != ""
}

func init() {
	if librechatHTTPKeysAvailable() {
		log.Println("librechat-keys: usando API HTTP de LibreChat (PUT /api/keys) para sincronizar claves")
	} else {
		log.Println("librechat-keys: LIBRECHAT_JWT_SECRET/JWT_SECRET no definidos — sync usará cifrado local (CREDS_KEY/CREDS_IV)")
	}
}
