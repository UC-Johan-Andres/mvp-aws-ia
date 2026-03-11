package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"launcher/config"
)

// HandleCheck validates the session cookie. Used by nginx auth_request.
func HandleCheck(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(config.CookieName)
	if err != nil || !ValidToken(cookie.Value) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleLogin handles GET (show form) and POST (process credentials).
// renderFn is injected to avoid a circular import with the ui package.
func HandleLogin(w http.ResponseWriter, r *http.Request, renderFn func(http.ResponseWriter, string)) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		user := r.FormValue("user")
		pass := r.FormValue("pass")
		if config.AuthPassword != "" && user == config.AuthUser && pass == config.AuthPassword {
			http.SetCookie(w, &http.Cookie{
				Name:     config.CookieName,
				Value:    MakeToken(),
				Path:     "/",
				Domain:   config.CookieDomain,
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   int(config.CookieTTL.Seconds()),
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		renderFn(w, "Credenciales incorrectas")
		return
	}
	renderFn(w, "")
}

// HandleLogout clears the session cookie and redirects to login.
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   config.CookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

// RequireAuth is a middleware that enforces authentication before calling next.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(config.CookieName)
		if err != nil || !ValidToken(cookie.Value) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// MakeToken creates a signed, time-limited session token.
func MakeToken() string {
	exp := fmt.Sprintf("%d", time.Now().Add(config.CookieTTL).Unix())
	return exp + "." + Sign(exp)
}

// ValidToken verifies the HMAC signature and expiry of a session token.
func ValidToken(tok string) bool {
	dot := strings.LastIndex(tok, ".")
	if dot < 0 {
		return false
	}
	payload, sig := tok[:dot], tok[dot+1:]
	if !hmac.Equal([]byte(Sign(payload)), []byte(sig)) {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

// Sign returns the HMAC-SHA256 hex digest of data using the session secret.
func Sign(data string) string {
	h := hmac.New(sha256.New, []byte(config.SessionSecret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
