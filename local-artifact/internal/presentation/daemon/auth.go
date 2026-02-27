package daemon

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const TokenCookieName = "ccsubagentsd_token"

type AuthOptions struct {
	AllowQueryBootstrap bool
	SkipPathPrefix      string
}

func AuthMiddleware(token string, next http.Handler, options AuthOptions) http.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.TrimSpace(options.SkipPathPrefix) != "" && strings.HasPrefix(r.URL.Path, options.SkipPathPrefix) {
			next.ServeHTTP(w, r)
			return
		}

		if hasValidToken(r, token) {
			next.ServeHTTP(w, r)
			return
		}

		if options.AllowQueryBootstrap {
			queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
			if queryToken != "" && secureEq(queryToken, token) && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
				http.SetCookie(w, &http.Cookie{
					Name:     TokenCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
					Secure:   r.TLS != nil,
				})

				redirect := *r.URL
				q := redirect.Query()
				q.Del("token")
				redirect.RawQuery = q.Encode()
				http.Redirect(w, r, sanitizeRedirectURL(&redirect), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(Envelope{OK: false, Error: &EnvelopeError{Code: CodeUnauthorized, Message: "missing or invalid token"}})
	})
}

func hasValidToken(r *http.Request, token string) bool {
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		provided := strings.TrimSpace(auth[len("Bearer "):])
		if secureEq(provided, token) {
			return true
		}
	}
	if c, err := r.Cookie(TokenCookieName); err == nil {
		if secureEq(strings.TrimSpace(c.Value), token) {
			return true
		}
	}
	return false
}

func secureEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func sanitizeRedirectURL(u *url.URL) string {
	if u == nil {
		return "/"
	}
	if strings.TrimSpace(u.RawQuery) == "" {
		return u.Path
	}
	return u.Path + "?" + u.RawQuery
}
