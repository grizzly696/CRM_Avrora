package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Role    string `json:"role"`
	Status  string `json:"status"`
	IsAdmin bool   `json:"isAdmin"`
}

type Service struct{ db *sql.DB }

type contextKey string

const userKey contextKey = "crm-user"

func New(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/logout" || r.URL.Path == "/api/auth/me" || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.userFromRequest(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/admin/") && !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/security/") && user.Role != "Информационная безопасность" && !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "security role required"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
		return
	}
	var user User
	var hash string
	var isAdmin int
	err := s.db.QueryRow(`SELECT id, name, email, role, status, password_hash, is_admin FROM users WHERE lower(email) = lower(?)`, strings.TrimSpace(input.Email)).Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Status, &hash, &isAdmin)
	if err != nil || user.Status != "active" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	user.IsAdmin = isAdmin == 1
	token, err := sessionToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	if _, err = s.db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`, token, user.ID, expires.Format(time.RFC3339Nano)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "crm_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: 86400})
	writeJSON(w, http.StatusOK, user)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("crm_session"); err == nil {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token = ?`, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "crm_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1, Expires: time.Unix(0, 0)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	user, err := s.userFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Service) User(r *http.Request) (User, bool) {
	user, ok := r.Context().Value(userKey).(User)
	return user, ok
}

func (s *Service) userFromRequest(r *http.Request) (User, error) {
	cookie, err := r.Cookie("crm_session")
	if err != nil {
		return User{}, err
	}
	var user User
	var isAdmin int
	var expires string
	err = s.db.QueryRow(`SELECT u.id, u.name, u.email, u.role, u.status, u.is_admin, se.expires_at FROM sessions se JOIN users u ON u.id = se.user_id WHERE se.token = ?`, cookie.Value).Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Status, &isAdmin, &expires)
	if err != nil {
		return User{}, err
	}
	when, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().UTC().After(when) || user.Status != "active" {
		return User{}, sql.ErrNoRows
	}
	user.IsAdmin = isAdmin == 1
	return user, nil
}

func sessionToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func HashPassword(password string) (string, error) {
	data, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(data), err
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
