package departments

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crm/internal/auth"
)

type Item struct {
	ID          int       `json:"id"`
	Role        string    `json:"role"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	DueAt       string    `json:"dueAt"`
	Amount      int       `json:"amount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Employee struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

type Module struct {
	db   *sql.DB
	auth *auth.Service
}

var kindsByRole = map[string][]string{
	"Кадры":      {"Вакансия", "Кандидат", "Адаптация", "Отпуск", "Документ"},
	"Разработка": {"Задача", "Дефект", "Релиз", "Технический долг"},
	"Информационная безопасность": {"Проверка", "Инцидент", "Доступ", "Риск"},
	"Бухгалтерия":                 {"Платёж", "Счёт", "Сверка", "Отчёт"},
	"Маркетинг":                   {"Кампания", "Контент", "Аналитика", "Лид"},
	"Продажи":                     {"Сделка", "Звонок", "Предложение", "Задача"},
}

func New(db *sql.DB, authService *auth.Service) *Module {
	return &Module{db: db, auth: authService}
}

func (m *Module) Name() string { return "departments" }

func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/department/items", m.list)
	mux.HandleFunc("POST /api/department/items", m.create)
	mux.HandleFunc("PATCH /api/department/items/{id}", m.update)
	mux.HandleFunc("GET /api/department/employees", m.employees)
}

func (m *Module) employees(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	if user.Role != "Кадры" && !canManageAll(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "employee directory access denied"})
		return
	}
	rows, err := m.db.Query(`SELECT id, name, email, role, status FROM users ORDER BY role, name`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list employees"})
		return
	}
	defer rows.Close()
	employees := make([]Employee, 0)
	for rows.Next() {
		var employee Employee
		if err = rows.Scan(&employee.ID, &employee.Name, &employee.Email, &employee.Role, &employee.Status); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read employees"})
			return
		}
		employees = append(employees, employee)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read employees"})
		return
	}
	writeJSON(w, http.StatusOK, employees)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	query := `SELECT id, role, kind, title, description, status, due_at, amount, created_at, updated_at FROM department_items`
	args := []any{}
	if !canManageAll(user) {
		query += ` WHERE role = ?`
		args = append(args, user.Role)
	}
	query += ` ORDER BY CASE status WHEN 'open' THEN 1 WHEN 'in_progress' THEN 2 ELSE 3 END, updated_at DESC, id DESC`
	rows, err := m.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list department work"})
		return
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		item, scanErr := scanItem(rows)
		if scanErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read department work"})
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read department work"})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	var input struct {
		Role        string `json:"role"`
		Kind        string `json:"kind"`
		Title       string `json:"title"`
		Description string `json:"description"`
		DueAt       string `json:"dueAt"`
		Amount      int    `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if canManageAll(user) {
		input.Role = strings.TrimSpace(input.Role)
	} else {
		input.Role = user.Role
	}
	input.Kind = strings.TrimSpace(input.Kind)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.DueAt = strings.TrimSpace(input.DueAt)
	if input.Title == "" || !validKind(input.Role, input.Kind) || input.Amount < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid role, work type, title and non-negative amount are required"})
		return
	}
	if input.DueAt != "" {
		if _, err := time.Parse("2006-01-02", input.DueAt); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "due date must use YYYY-MM-DD"})
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := m.db.Exec(`INSERT INTO department_items (role, kind, title, description, status, due_at, amount, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', ?, ?, ?, ?, ?)`,
		input.Role, input.Kind, input.Title, input.Description, input.DueAt, input.Amount, user.ID, now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create department work"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create department work"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid work item id"})
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil || !validStatus(input.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid work status"})
		return
	}
	query := `UPDATE department_items SET status = ?, updated_at = ? WHERE id = ?`
	args := []any{input.Status, time.Now().UTC().Format(time.RFC3339Nano), id}
	if !canManageAll(user) {
		query += ` AND role = ?`
		args = append(args, user.Role)
	}
	result, err := m.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update department work"})
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update department work"})
		return
	}
	if changed == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "work item not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": input.Status})
}

func scanItem(row interface{ Scan(...any) error }) (Item, error) {
	var item Item
	var created, updated string
	err := row.Scan(&item.ID, &item.Role, &item.Kind, &item.Title, &item.Description, &item.Status, &item.DueAt, &item.Amount, &created, &updated)
	if err == nil {
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	if err == nil {
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	return item, err
}

func validKind(role, kind string) bool {
	for _, allowed := range kindsByRole[role] {
		if kind == allowed {
			return true
		}
	}
	return false
}

func validStatus(status string) bool {
	return status == "open" || status == "in_progress" || status == "done"
}

func canManageAll(user auth.User) bool {
	return user.IsAdmin || user.Role == "Менеджмент"
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
