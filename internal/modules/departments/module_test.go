package departments

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

type apiTest struct {
	handler http.Handler
	db      *sql.DB
	users   map[string]int
}

func newAPITest(t *testing.T) *apiTest {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	users := make(map[string]int)
	for _, role := range []string{"Менеджмент", "Кадры", "Разработка", "Информационная безопасность", "Бухгалтерия", "Маркетинг", "Продажи"} {
		var id int
		err := store.DB.QueryRow(`SELECT id FROM users WHERE role = ? ORDER BY id LIMIT 1`, role).Scan(&id)
		if err != nil {
			t.Fatalf("find seeded user for %s: %v", role, err)
		}
		users[role] = id
	}

	authService := auth.New(store.DB)
	mux := http.NewServeMux()
	authService.Routes(mux)
	New(store.DB, authService).Routes(mux)
	return &apiTest{handler: authService.Middleware(mux), db: store.DB, users: users}
}

func (api *apiTest) request(t *testing.T, userID int, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	token := "test-session"
	_, err := api.db.Exec(`INSERT OR REPLACE INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.AddCookie(&http.Cookie{Name: "crm_session", Value: token})
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	return response
}

func TestDepartmentItemPermissionsAndWorkflow(t *testing.T) {
	api := newAPITest(t)
	hrID := api.users["Кадры"]
	developerID := api.users["Разработка"]
	managerID := api.users["Менеджмент"]

	unauthorized := httptest.NewRecorder()
	api.handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/department/items", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	hrItems := api.request(t, hrID, http.MethodGet, "/api/department/items", "")
	if hrItems.Code != http.StatusOK {
		t.Fatalf("HR list status = %d, want %d: %s", hrItems.Code, http.StatusOK, hrItems.Body)
	}
	var items []Item
	if err := json.Unmarshal(hrItems.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode HR items: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected seeded HR work")
	}
	for _, item := range items {
		if item.Role != "Кадры" {
			t.Errorf("HR received item for role %q", item.Role)
		}
	}

	forgedRole := api.request(t, hrID, http.MethodPost, "/api/department/items",
		`{"role":"Бухгалтерия","kind":"Счёт","title":"HR-owned item"}`)
	if forgedRole.Code != http.StatusBadRequest {
		t.Fatalf("forged-role create status = %d, want %d", forgedRole.Code, http.StatusBadRequest)
	}
	forcedHRRole := api.request(t, hrID, http.MethodPost, "/api/department/items",
		`{"role":"Бухгалтерия","kind":"Адаптация","title":"Собственная кадровая запись"}`)
	if forcedHRRole.Code != http.StatusCreated {
		t.Fatalf("HR create status = %d, want %d: %s", forcedHRRole.Code, http.StatusCreated, forcedHRRole.Body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(forcedHRRole.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created item: %v", err)
	}

	devCannotUpdateHR := api.request(t, developerID, http.MethodPatch,
		"/api/department/items/"+jsonNumber(created.ID), `{"status":"done"}`)
	if devCannotUpdateHR.Code != http.StatusNotFound {
		t.Fatalf("cross-role update status = %d, want %d", devCannotUpdateHR.Code, http.StatusNotFound)
	}
	hrCanStart := api.request(t, hrID, http.MethodPatch,
		"/api/department/items/"+jsonNumber(created.ID), `{"status":"in_progress"}`)
	if hrCanStart.Code != http.StatusOK {
		t.Fatalf("HR status update = %d, want %d: %s", hrCanStart.Code, http.StatusOK, hrCanStart.Body)
	}
	hrCanFinish := api.request(t, hrID, http.MethodPatch,
		"/api/department/items/"+jsonNumber(created.ID), `{"status":"done"}`)
	if hrCanFinish.Code != http.StatusOK {
		t.Fatalf("HR finish status = %d, want %d: %s", hrCanFinish.Code, http.StatusOK, hrCanFinish.Body)
	}

	managerItems := api.request(t, managerID, http.MethodGet, "/api/department/items", "")
	if managerItems.Code != http.StatusOK {
		t.Fatalf("manager list status = %d, want %d: %s", managerItems.Code, http.StatusOK, managerItems.Body)
	}
	if err := json.Unmarshal(managerItems.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode manager items: %v", err)
	}
	seenHR, seenDeveloper := false, false
	for _, item := range items {
		seenHR = seenHR || item.Role == "Кадры"
		seenDeveloper = seenDeveloper || item.Role == "Разработка"
	}
	if !seenHR || !seenDeveloper {
		t.Fatalf("manager list must contain multiple departments (HR=%t, developer=%t)", seenHR, seenDeveloper)
	}
}

func TestDepartmentItemValidation(t *testing.T) {
	api := newAPITest(t)
	userID := api.users["Маркетинг"]
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{`},
		{name: "missing title", body: `{"kind":"Кампания"}`},
		{name: "unknown kind", body: `{"kind":"Счёт","title":"Wrong kind"}`},
		{name: "negative amount", body: `{"kind":"Кампания","title":"Negative","amount":-1}`},
		{name: "invalid due date", body: `{"kind":"Кампания","title":"Bad date","dueAt":"12-10-2026"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := api.request(t, userID, http.MethodPost, "/api/department/items", test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("create status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body)
			}
		})
	}

	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/api/department/items/not-a-number", body: `{"status":"done"}`},
		{path: "/api/department/items/1", body: `{"status":"unknown"}`},
	} {
		response := api.request(t, userID, http.MethodPatch, test.path, test.body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("PATCH %s status = %d, want %d", test.path, response.Code, http.StatusBadRequest)
		}
	}
}

func TestEmployeeDirectoryAccess(t *testing.T) {
	api := newAPITest(t)
	for _, test := range []struct {
		role       string
		wantStatus int
	}{
		{role: "Кадры", wantStatus: http.StatusOK},
		{role: "Менеджмент", wantStatus: http.StatusOK},
		{role: "Маркетинг", wantStatus: http.StatusForbidden},
	} {
		t.Run(test.role, func(t *testing.T) {
			response := api.request(t, api.users[test.role], http.MethodGet, "/api/department/employees", "")
			if response.Code != test.wantStatus {
				t.Fatalf("directory status = %d, want %d: %s", response.Code, test.wantStatus, response.Body)
			}
			if test.wantStatus == http.StatusOK {
				var employees []Employee
				if err := json.Unmarshal(response.Body.Bytes(), &employees); err != nil {
					t.Fatalf("decode employees: %v", err)
				}
				if len(employees) < 6 {
					t.Errorf("employee directory returned %d users, want at least 6", len(employees))
				}
			}
		})
	}
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
