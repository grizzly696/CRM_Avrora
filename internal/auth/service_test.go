package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"crm/internal/auth"
	"crm/internal/storage"
)

func TestLoginMeAndLogout(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	service := auth.New(store.DB)
	mux := http.NewServeMux()
	service.Routes(mux)
	handler := service.Middleware(mux)

	badLogin := httptest.NewRecorder()
	handler.ServeHTTP(badLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		bytes.NewBufferString(`{"email":"alexey@northstar.local","password":"wrong-password"}`)))
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login status = %d, want %d", badLogin.Code, http.StatusUnauthorized)
	}

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		bytes.NewBufferString(`{"email":"alexey@northstar.local","password":"Northstar123!"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("valid login status = %d, want %d: %s", login.Code, http.StatusOK, login.Body)
	}
	var user auth.User
	if err := json.Unmarshal(login.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode login user: %v", err)
	}
	if !user.IsAdmin || user.Role != "Менеджмент" {
		t.Fatalf("unexpected login user: %+v", user)
	}
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "crm_session" {
			if !cookie.HttpOnly || cookie.Value == "" {
				t.Fatalf("session cookie is not protected: %+v", cookie)
			}
			loginCookie := cookie
			me := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			request.AddCookie(loginCookie)
			handler.ServeHTTP(me, request)
			if me.Code != http.StatusOK {
				t.Fatalf("authenticated profile status = %d, want %d", me.Code, http.StatusOK)
			}

			logout := httptest.NewRecorder()
			logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
			logoutRequest.AddCookie(loginCookie)
			handler.ServeHTTP(logout, logoutRequest)
			if logout.Code != http.StatusOK {
				t.Fatalf("logout status = %d, want %d", logout.Code, http.StatusOK)
			}

			expiredSession := httptest.NewRecorder()
			expiredRequest := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
			expiredRequest.AddCookie(loginCookie)
			handler.ServeHTTP(expiredSession, expiredRequest)
			if expiredSession.Code != http.StatusUnauthorized {
				t.Fatalf("logged-out request status = %d, want %d", expiredSession.Code, http.StatusUnauthorized)
			}
			return
		}
	}
	t.Fatal("login response did not set a session cookie")
}
