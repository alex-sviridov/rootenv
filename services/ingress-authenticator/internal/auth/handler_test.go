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
	ownerID     string // returned by GetAttempt; defaults to userID when empty
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
	owner := f.ownerID
	if owner == "" {
		owner = f.userID
	}
	return owner, nil
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

func TestHandler_success_no_trailing_segment(t *testing.T) {
	// URI with exactly three segments: /relay/exec/<id>
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/relay/exec/atm_123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
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
	cases := []struct {
		name string
		uri  string
	}{
		{"wrong prefix", "/not/the/right/pattern/"},
		{"only relay", "/relay/"},
		{"exec missing", "/relay/notexec/atm_123/"},
		{"empty attempt id", "/relay/exec//server-0/"},
		{"exec in wrong position", "/foo/exec/atm_123/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pb := &fakePB{userID: "usr_abc"}
			h := auth.NewHandler(pb)
			req := makeReq("tok123", tc.uri)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: want 400, got %d", tc.uri, w.Code)
			}
		})
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

// TestHandler_owner_mismatch verifies the defense-in-depth cross-check:
// even if PocketBase's viewRule were misconfigured to allow access,
// the handler rejects a response where the attempt's owner differs from the authenticated user.
func TestHandler_owner_mismatch(t *testing.T) {
	pb := &fakePB{userID: "usr_abc", ownerID: "usr_other"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/relay/exec/atm_other/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403 on owner mismatch, got %d", w.Code)
	}
}
