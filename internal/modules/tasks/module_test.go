package tasks

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

func TestTaskCreateClaimAndComplete(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	taskTestUser(t, store.DB, "Менеджмент", "manager-session")
	taskTestUser(t, store.DB, "Разработка", "developer-session")
	taskTestUser(t, store.DB, "Информационная безопасность", "security-session")
	service := auth.New(store.DB)
	mux := http.NewServeMux()
	service.Routes(mux)
	New(store.DB, service).Routes(mux)
	handler := service.Middleware(mux)
	request := func(token, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "crm_session", Value: token})
		handler.ServeHTTP(response, req)
		return response
	}

	created := request("manager-session", http.MethodPost, "/api/tasks",
		`{"title":"Test sprint item","description":"Verify task lifecycle","priority":"high","groupRole":"Разработка"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", created.Code, http.StatusCreated, created.Body)
	}
	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	taskPath := "/api/tasks/" + strconv.Itoa(result.ID)

	securityList := request("security-session", http.MethodGet, "/api/tasks", "")
	if securityList.Code != http.StatusOK || bytes.Contains(securityList.Body.Bytes(), []byte("Test sprint item")) {
		t.Fatalf("unassigned task leaked outside its group: %d %s", securityList.Code, securityList.Body)
	}
	claimed := request("developer-session", http.MethodPost, taskPath+"/claim", "")
	if claimed.Code != http.StatusOK {
		t.Fatalf("claim status = %d, want %d: %s", claimed.Code, http.StatusOK, claimed.Body)
	}
	cannotSteal := request("security-session", http.MethodPost, taskPath+"/claim", "")
	if cannotSteal.Code != http.StatusConflict {
		t.Fatalf("second claim status = %d, want %d", cannotSteal.Code, http.StatusConflict)
	}
	completed := request("developer-session", http.MethodPatch, taskPath, `{"status":"done"}`)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want %d: %s", completed.Code, http.StatusOK, completed.Body)
	}
}

func TestTaskCreateRejectsInvalidAssignment(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	taskTestUser(t, store.DB, "Менеджмент", "manager-session")
	service := auth.New(store.DB)
	mux := http.NewServeMux()
	service.Routes(mux)
	New(store.DB, service).Routes(mux)
	handler := service.Middleware(mux)

	for _, body := range []string{
		`{`,
		`{"title":"bad group","groupRole":"Unknown department"}`,
		`{"title":"both targets","groupRole":"Разработка","assigneeId":2}`,
		`{"title":"bad priority","priority":"urgent"}`,
	} {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "crm_session", Value: "manager-session"})
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}
}

func taskTestUser(t *testing.T, db *sql.DB, role, token string) int {
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
