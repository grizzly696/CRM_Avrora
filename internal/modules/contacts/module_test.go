package contacts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"crm/internal/storage"
)

func TestCreateFilterAndStats(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	mux := http.NewServeMux()
	New(store.DB).Routes(mux)

	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/contacts",
		bytes.NewBufferString(`{"name":"  Test Lead  ","company":" QA Inc ","email":"qa@example.test","value":12500}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", create.Code, http.StatusCreated, create.Body)
	}
	var contact Contact
	if err := json.Unmarshal(create.Body.Bytes(), &contact); err != nil {
		t.Fatalf("decode created contact: %v", err)
	}
	if contact.Name != "Test Lead" || contact.Company != "QA Inc" || contact.Status != "lead" || contact.Value != 12500 {
		t.Fatalf("unexpected created contact: %+v", contact)
	}

	filtered := httptest.NewRecorder()
	mux.ServeHTTP(filtered, httptest.NewRequest(http.MethodGet, "/api/contacts?q=qa+inc&status=lead", nil))
	if filtered.Code != http.StatusOK {
		t.Fatalf("filter status = %d, want %d", filtered.Code, http.StatusOK)
	}
	var contacts []Contact
	if err := json.Unmarshal(filtered.Body.Bytes(), &contacts); err != nil {
		t.Fatalf("decode filtered contacts: %v", err)
	}
	if len(contacts) != 1 || contacts[0].ID != contact.ID {
		t.Fatalf("filtered contacts = %+v, want only created contact", contacts)
	}

	stats := httptest.NewRecorder()
	mux.ServeHTTP(stats, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	var values struct {
		Contacts int `json:"contacts"`
		Leads    int `json:"leads"`
		Pipeline int `json:"pipeline"`
	}
	if err := json.Unmarshal(stats.Body.Bytes(), &values); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if values.Contacts < 4 || values.Leads < 1 || values.Pipeline < contact.Value {
		t.Fatalf("unexpected stats after create: %+v", values)
	}
}

func TestCreateRejectsInvalidContact(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mux := http.NewServeMux()
	New(store.DB).Routes(mux)

	for _, body := range []string{
		`{`,
		`{"name":"  "}`,
		`{"name":"Bad status","status":"closed"}`,
		`{"name":"Negative value","value":-1}`,
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/contacts", bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}
}
