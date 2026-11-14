package tasks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crm/internal/auth"
)

type Task struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    string    `json:"priority"`
	Status      string    `json:"status"`
	AssigneeID  *int      `json:"assigneeId,omitempty"`
	Assignee    string    `json:"assignee,omitempty"`
	GroupRole   string    `json:"groupRole,omitempty"`
	CreatedBy   int       `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type UserOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type Module struct {
	db   *sql.DB
	auth *auth.Service
}

var groups = []string{"Менеджмент", "Кадры", "Разработка", "Информационная безопасность", "Бухгалтерия", "Маркетинг", "Продажи"}

func New(db *sql.DB, authService *auth.Service) *Module { return &Module{db: db, auth: authService} }
func (m *Module) Name() string                          { return "tasks" }
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks", m.list)
	mux.HandleFunc("POST /api/tasks", m.create)
	mux.HandleFunc("PATCH /api/tasks/{id}", m.update)
	mux.HandleFunc("POST /api/tasks/{id}/claim", m.claim)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	rows, err := m.db.Query(`SELECT t.id, t.title, t.description, t.priority, t.status, t.assignee_id, COALESCE(u.name, ''), t.group_role, t.created_by, t.created_at, t.updated_at FROM tasks t LEFT JOIN users u ON u.id = t.assignee_id WHERE t.assignee_id = ? OR t.group_role = ? OR t.created_by = ? OR ? = 1 OR ? = 1 ORDER BY CASE t.priority WHEN 'critical' THEN 1 WHEN 'high' THEN 2 WHEN 'medium' THEN 3 ELSE 4 END, t.updated_at DESC`, user.ID, user.Role, user.ID, boolInt(user.IsAdmin), boolInt(user.Role == "Менеджмент"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tasks"})
		return
	}
	defer rows.Close()
	items := make([]Task, 0)
	for rows.Next() {
		item, scanErr := scan(rows)
		if scanErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read tasks"})
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read tasks"})
		return
	}
	users := make([]UserOption, 0)
	userRows, err := m.db.Query(`SELECT id, name, role FROM users WHERE status = 'active' ORDER BY name`)
	if err == nil {
		defer userRows.Close()
		for userRows.Next() {
			var option UserOption
			if userRows.Scan(&option.ID, &option.Name, &option.Role) == nil {
				users = append(users, option)
			}
		}
		if err = userRows.Err(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read assignees"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items, "users": users, "groups": groups})
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		AssigneeID  int    `json:"assigneeId"`
		GroupRole   string `json:"groupRole"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Title) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	input.Title, input.Description, input.Priority, input.GroupRole = strings.TrimSpace(input.Title), strings.TrimSpace(input.Description), strings.ToLower(strings.TrimSpace(input.Priority)), strings.TrimSpace(input.GroupRole)
	if input.Priority == "" {
		input.Priority = "medium"
	}
	if !validPriority(input.Priority) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid priority"})
		return
	}
	if input.AssigneeID > 0 && input.GroupRole != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "choose employee or group"})
		return
	}
	if input.GroupRole != "" && !validGroup(input.GroupRole) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group"})
		return
	}
	if input.AssigneeID > 0 {
		var exists int
		if err := m.db.QueryRow(`SELECT id FROM users WHERE id = ? AND status = 'active'`, input.AssigneeID).Scan(&exists); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "employee not found"})
			return
		}
	}
	now := time.Now().UTC()
	result, err := m.db.Exec(`INSERT INTO tasks (title, description, priority, status, assignee_id, group_role, created_by, created_at, updated_at) VALUES (?, ?, ?, 'todo', NULLIF(?, 0), ?, ?, ?, ?)`, input.Title, input.Description, input.Priority, input.AssigneeID, input.GroupRole, user.ID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create task"})
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "message": "task created"})
}

func (m *Module) claim(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}
	result, err := m.db.Exec(`UPDATE tasks SET assignee_id = ?, group_role = '', status = 'in_progress', updated_at = ? WHERE id = ? AND (assignee_id IS NULL OR assignee_id = ?) AND (group_role = '' OR group_role = ?)`, user.ID, time.Now().UTC().Format(time.RFC3339Nano), id, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to claim task"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "task is not available for you"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "task claimed"})
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil || !validStatus(input.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	result, err := m.db.Exec(`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ? AND (assignee_id = ? OR created_by = ? OR ? = 1 OR ? = 1)`, input.Status, time.Now().UTC().Format(time.RFC3339Nano), id, user.ID, user.ID, boolInt(user.IsAdmin), boolInt(user.Role == "Менеджмент"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update task"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "task access denied"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "task updated"})
}

func scan(row interface{ Scan(...any) error }) (Task, error) {
	var item Task
	var assigneeID sql.NullInt64
	var created, updated string
	err := row.Scan(&item.ID, &item.Title, &item.Description, &item.Priority, &item.Status, &assigneeID, &item.Assignee, &item.GroupRole, &item.CreatedBy, &created, &updated)
	if assigneeID.Valid {
		value := int(assigneeID.Int64)
		item.AssigneeID = &value
	}
	if err == nil {
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	if err == nil {
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	return item, err
}
func validPriority(value string) bool {
	return value == "critical" || value == "high" || value == "medium" || value == "low"
}
func validStatus(value string) bool {
	return value == "todo" || value == "in_progress" || value == "done"
}
func validGroup(value string) bool {
	for _, item := range groups {
		if item == value {
			return true
		}
	}
	return false
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
