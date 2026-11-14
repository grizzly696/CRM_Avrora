package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crm/internal/auth"
)

type User struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"lastSeen"`
}
type AuditEvent struct {
	ID        int       `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"createdAt"`
}
type Module struct {
	db   *sql.DB
	auth *auth.Service
}

var roles = []string{"Менеджмент", "Кадры", "Разработка", "Информационная безопасность", "Бухгалтерия", "Маркетинг", "Продажи"}

func New(db *sql.DB, authService *auth.Service) *Module {
	return &Module{db: db, auth: authService}
}
func (m *Module) Name() string { return "admin" }
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/overview", m.overview)
	mux.HandleFunc("GET /api/admin/users", m.listUsers)
	mux.HandleFunc("POST /api/admin/users", m.createUser)
	mux.HandleFunc("PATCH /api/admin/users/{id}", m.updateUser)
	mux.HandleFunc("GET /api/admin/audit", m.listAudit)
}

func (m *Module) overview(w http.ResponseWriter, r *http.Request) {
	var users, active int
	if err := m.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'active'), 0) FROM users`).Scan(&users, &active); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate users"})
		return
	}
	byRole := make(map[string]int, len(roles))
	rows, err := m.db.Query(`SELECT role, COUNT(*) FROM users GROUP BY role`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate roles"})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		var count int
		if err = rows.Scan(&role, &count); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate roles"})
			return
		}
		byRole[role] = count
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate roles"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "active": active, "inactive": users - active, "roles": len(roles), "byRole": byRole})
}

func (m *Module) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(`SELECT id, name, email, role, status, last_seen FROM users ORDER BY name`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list users"})
		return
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read users"})
			return
		}
		users = append(users, user)
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles, "users": users})
}

func (m *Module) createUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := m.auth.User(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	var input struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and email are required"})
		return
	}
	input.Name, input.Email, input.Role = strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Email)), strings.TrimSpace(input.Role)
	if !validRole(input.Role) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown role"})
		return
	}
	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to secure password"})
		return
	}
	lastSeen := time.Now().UTC()
	tx, err := m.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO users (name, email, role, status, last_seen, password_hash) VALUES (?, ?, ?, 'active', ?, ?)`, input.Name, input.Email, input.Role, lastSeen.Format(time.RFC3339Nano), passwordHash)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "email already exists"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events (actor, action, target, created_at) VALUES (?, ?, ?, ?)`, actor.Name, "Добавил пользователя", input.Name, lastSeen.Format(time.RFC3339Nano)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write audit"})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}
	writeJSON(w, http.StatusCreated, User{ID: int(id), Name: input.Name, Email: input.Email, Role: input.Role, Status: "active", LastSeen: lastSeen})
}

func (m *Module) updateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := m.auth.User(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	var input struct {
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if input.Role != "" && !validRole(input.Role) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown role"})
		return
	}
	if input.Status != "" && input.Status != "active" && input.Status != "inactive" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown status"})
		return
	}
	if input.Role == "" && input.Status == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one field is required"})
		return
	}
	tx, err := m.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE users SET role = COALESCE(NULLIF(?, ''), role), status = COALESCE(NULLIF(?, ''), status) WHERE id = ?`, input.Role, input.Status, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
		return
	}
	if affected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	var user User
	var lastSeen string
	err = tx.QueryRowContext(r.Context(), `SELECT id, name, email, role, status, last_seen FROM users WHERE id = ?`, id).Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Status, &lastSeen)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read user"})
		return
	}
	user.LastSeen, err = time.Parse(time.RFC3339Nano, lastSeen)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid user date"})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events (actor, action, target, created_at) VALUES (?, ?, ?, ?)`, actor.Name, "Обновил пользователя", user.Name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write audit"})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (m *Module) listAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(`SELECT id, actor, action, target, created_at FROM audit_events ORDER BY created_at DESC, id DESC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list audit"})
		return
	}
	defer rows.Close()
	events := make([]AuditEvent, 0)
	for rows.Next() {
		var event AuditEvent
		var created string
		if err = rows.Scan(&event.ID, &event.Actor, &event.Action, &event.Target, &created); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read audit"})
			return
		}
		event.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid audit date"})
			return
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read audit"})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func validRole(role string) bool {
	for _, item := range roles {
		if role == item {
			return true
		}
	}
	return false
}
func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var user User
	var lastSeen string
	err := row.Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Status, &lastSeen)
	if err == nil {
		user.LastSeen, err = time.Parse(time.RFC3339Nano, lastSeen)
	}
	return user, err
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
