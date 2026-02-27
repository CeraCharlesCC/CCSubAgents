package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddleware_UnauthorizedWithoutToken(t *testing.T) {
	h := AuthMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), AuthOptions{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_QueryBootstrapSetsCookieAndRedirects(t *testing.T) {
	h := AuthMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), AuthOptions{AllowQueryBootstrap: true})

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodGet, "/?token=secret", nil)
	h.ServeHTTP(first, firstReq)
	if first.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status, got %d", first.Code)
	}
	cookie := first.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected auth cookie to be set")
	}
	if strings.Contains(first.Header().Get("Location"), "token=") {
		t.Fatalf("expected redirect location without token query, got %q", first.Header().Get("Location"))
	}

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodGet, "/", nil)
	secondReq.AddCookie(cookie[0])
	h.ServeHTTP(second, secondReq)
	if second.Code != http.StatusOK {
		t.Fatalf("expected authenticated request to succeed, got %d", second.Code)
	}
}
