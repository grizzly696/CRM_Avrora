package contacts

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Contact struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Company   string    `json:"company"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	Value     int       `json:"value"`
	CreatedAt time.Time `json:"createdAt"`
}

type Module struct{ db *sql.DB }

func New(db *sql.DB) *Module { return &Module{db: db} }

func (m *Module) Name() string { return "contacts" }

func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/contacts", m.list)
	mux.HandleFunc("POST /api/contacts", m.create)
	mux.HandleFunc("GET /api/stats", m.stats)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	rows, err := m.db.Query(`SELECT id, name, company, email, status, value, created_at FROM contacts
WHERE (? = '' OR lower(name || ' ' || company || ' ' || email) LIKE '%' || ? || '%')
AND (? = '' OR status = ?) ORDER BY created_at DESC, id DESC`, query, query, status, status)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list contacts"})
		return
	}
	defer rows.Close()
	result := make([]Contact, 0)
	for rows.Next() {
		contact, scanErr := scanContact(rows)
		if scanErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read contacts"})
			return
		}
		result = append(result, contact)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read contacts"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    string `json:"name"`
		Company string `json:"company"`
		Email   string `json:"email"`
		Status  string `json:"status"`
		Value   int    `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Company = strings.TrimSpace(input.Company)
	input.Email = strings.TrimSpace(input.Email)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "lead"
	}
	if (input.Status != "lead" && input.Status != "active") || input.Value < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status or value"})
		return
	}
	createdAt := time.Now().UTC()
	result, err := m.db.Exec(`INSERT INTO contacts (name, company, email, status, value, created_at) VALUES (?, ?, ?, ?, ?, ?)`, input.Name, input.Company, input.Email, input.Status, input.Value, createdAt.Format(time.RFC3339Nano))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create contact"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create contact"})
		return
	}
	writeJSON(w, http.StatusCreated, Contact{ID: int(id), Name: input.Name, Company: input.Company, Email: input.Email, Status: input.Status, Value: input.Value, CreatedAt: createdAt})
}

func (m *Module) stats(w http.ResponseWriter, r *http.Request) {
	var stats struct {
		Contacts int `json:"contacts"`
		Active   int `json:"active"`
		Leads    int `json:"leads"`
		Pipeline int `json:"pipeline"`
	}
	err := m.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'active'), 0), COALESCE(SUM(status = 'lead'), 0), COALESCE(SUM(value), 0) FROM contacts`).Scan(&stats.Contacts, &stats.Active, &stats.Leads, &stats.Pipeline)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate stats"})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

type scanner interface{ Scan(dest ...any) error }

func scanContact(row scanner) (Contact, error) {
	var contact Contact
	var created string
	err := row.Scan(&contact.ID, &contact.Name, &contact.Company, &contact.Email, &contact.Status, &contact.Value, &created)
	if err == nil {
		contact.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	return contact, err
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
