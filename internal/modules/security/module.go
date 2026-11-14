package security

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crm/internal/auth"
)

type Vulnerability struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Severity    string    `json:"severity"`
	Status      string    `json:"status"`
	Owner       string    `json:"owner"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Module struct {
	db   *sql.DB
	auth *auth.Service
}

func New(db *sql.DB, authService *auth.Service) *Module { return &Module{db: db, auth: authService} }
func (m *Module) Name() string                          { return "security" }
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/security/vulnerabilities", m.list)
	mux.HandleFunc("POST /api/security/vulnerabilities", m.create)
	mux.HandleFunc("PATCH /api/security/vulnerabilities/{id}", m.update)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(`SELECT id, title, description, severity, status, owner, created_at, updated_at FROM vulnerabilities ORDER BY CASE severity WHEN 'critical' THEN 1 WHEN 'high' THEN 2 WHEN 'medium' THEN 3 ELSE 4 END, updated_at DESC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list vulnerabilities"})
		return
	}
	defer rows.Close()
	items := make([]Vulnerability, 0)
	for rows.Next() {
		item, scanErr := scan(rows)
		if scanErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read vulnerabilities"})
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read vulnerabilities"})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Severity    string `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Description) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and description are required"})
		return
	}
	input.Title, input.Description, input.Severity = strings.TrimSpace(input.Title), strings.TrimSpace(input.Description), strings.ToLower(strings.TrimSpace(input.Severity))
	if !validSeverity(input.Severity) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid severity"})
		return
	}
	user, ok := m.auth.User(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	now := time.Now().UTC()
	result, err := m.db.Exec(`INSERT INTO vulnerabilities (title, description, severity, status, owner, created_at, updated_at) VALUES (?, ?, ?, 'open', ?, ?, ?)`, input.Title, input.Description, input.Severity, user.Name, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create vulnerability"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create vulnerability"})
		return
	}
	writeJSON(w, http.StatusCreated, Vulnerability{ID: int(id), Title: input.Title, Description: input.Description, Severity: input.Severity, Status: "open", Owner: user.Name, CreatedAt: now, UpdatedAt: now})
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid vulnerability id"})
		return
	}
	var input struct {
		Status   string `json:"status"`
		Severity string `json:"severity"`
	}
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if input.Status != "" && !validStatus(input.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	if input.Severity != "" && !validSeverity(input.Severity) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid severity"})
		return
	}
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := m.db.Exec(`UPDATE vulnerabilities SET status = COALESCE(NULLIF(?, ''), status), severity = COALESCE(NULLIF(?, ''), severity), updated_at = ? WHERE id = ?`, input.Status, input.Severity, updated, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update vulnerability"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "vulnerability not found"})
		return
	}
	var item Vulnerability
	var created, changedAt string
	err = m.db.QueryRow(`SELECT id, title, description, severity, status, owner, created_at, updated_at FROM vulnerabilities WHERE id = ?`, id).Scan(&item.ID, &item.Title, &item.Description, &item.Severity, &item.Status, &item.Owner, &created, &changedAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read vulnerability"})
		return
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, changedAt)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid vulnerability date"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func validSeverity(value string) bool {
	return value == "critical" || value == "high" || value == "medium" || value == "low"
}
func validStatus(value string) bool {
	return value == "open" || value == "in_progress" || value == "resolved"
}
func scan(row interface{ Scan(...any) error }) (Vulnerability, error) {
	var item Vulnerability
	var created, updated string
	err := row.Scan(&item.ID, &item.Title, &item.Description, &item.Severity, &item.Status, &item.Owner, &created, &updated)
	if err == nil {
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	if err == nil {
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	return item, err
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
