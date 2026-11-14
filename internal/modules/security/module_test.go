package security

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"crm/internal/auth"
	"crm/internal/storage"
)

func TestVulnerabilityWorkflowAndRoleGuard(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	userID := insertSecuritySession(t, store.DB, "Информационная безопасность", "security-session")
	marketingID := insertSecuritySession(t, store.DB, "Маркетинг", "marketing-session")
	service := auth.New(store.DB)
	mux := http.NewServeMux()
	service.Routes(mux)
	New(store.DB, service).Routes(mux)
	handler := service.Middleware(mux)

	request := func(userID int, token, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "crm_session", Value: token})
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	denied := request(marketingID, "marketing-session", http.MethodGet, "/api/security/vulnerabilities", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-security role list status = %d, want %d", denied.Code, http.StatusForbidden)
	}
	created := request(userID, "security-session", http.MethodPost, "/api/security/vulnerabilities",
		`{"title":"Test finding","description":"Check the affected system","severity":"high"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", created.Code, http.StatusCreated, created.Body)
	}
	listed := request(userID, "security-session", http.MethodGet, "/api/security/vulnerabilities", "")
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte("Test finding")) {
		t.Fatalf("list did not return created finding: status %d, body %s", listed.Code, listed.Body)
	}
	var finding struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &finding); err != nil {
		t.Fatalf("decode created finding: %v", err)
	}
	updated := request(userID, "security-session", http.MethodPatch, "/api/security/vulnerabilities/"+strconv.Itoa(finding.ID),
		`{"status":"in_progress","severity":"critical"}`)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"status":"in_progress"`)) {
		t.Fatalf("update response = %d, %s", updated.Code, updated.Body)
	}
	invalid := request(userID, "security-session", http.MethodPatch, "/api/security/vulnerabilities/"+strconv.Itoa(finding.ID),
		`{"status":"invalid"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status response = %d, want %d", invalid.Code, http.StatusBadRequest)
	}
}

func insertSecuritySession(t *testing.T, db *sql.DB, role, token string) int {
	t.Helper()
	var userID int
	if err := db.QueryRow(`SELECT id FROM users WHERE role = ? ORDER BY id LIMIT 1`, role).Scan(&userID); err != nil {
		t.Fatalf("find user for role %s: %v", role, err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert session for %s: %v", role, err)
	}
	return userID
}
