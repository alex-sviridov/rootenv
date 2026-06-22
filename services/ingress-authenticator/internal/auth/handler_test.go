package auth_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexsviridov/linuxlab/ingress-authenticator/internal/auth"
)

type fakePB struct {
	userID      string
	validateErr error
	attemptErr  error
}

func (f *fakePB) ValidateToken(token string) (string, error) {
	return f.userID, f.validateErr
}

func (f *fakePB) GetAttempt(token, attemptID string) (string, error) {
	if f.attemptErr != nil {
		return "", f.attemptErr
	}
	return f.userID, nil
}

func makeReq(cookie, forwardedURI string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "pb_auth", Value: cookie})
	}
	if forwardedURI != "" {
		req.Header.Set("X-Forwarded-Uri", forwardedURI)
	}
	return req
}

func TestHandler_success(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/relay/exec/atm_123/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	if w.Header().Get("X-User-Id") != "usr_abc" {
		t.Errorf("want X-User-Id usr_abc, got %q", w.Header().Get("X-User-Id"))
	}
	if w.Header().Get("X-Attempt-Id") != "atm_123" {
		t.Errorf("want X-Attempt-Id atm_123, got %q", w.Header().Get("X-Attempt-Id"))
	}
}

func TestHandler_missing_cookie(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("", "/relay/exec/atm_123/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestHandler_missing_forwarded_uri(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandler_unparseable_uri(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/not/the/right/pattern/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandler_invalid_token(t *testing.T) {
	pb := &fakePB{validateErr: fmt.Errorf("unauthorized")}
	h := auth.NewHandler(pb)

	req := makeReq("badtok", "/relay/exec/atm_123/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestHandler_forbidden_attempt(t *testing.T) {
	pb := &fakePB{userID: "usr_abc", attemptErr: fmt.Errorf("forbidden")}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/relay/exec/atm_other/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", w.Code)
	}
}
