package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if _, err = db.Exec(`PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure database: %w", err)
	}
	if err = migrate(db, path); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return &Store{DB: db}, nil
}

func migrate(db *sql.DB, path string) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    company TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'lead',
    value INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'FILE',
    size TEXT NOT NULL DEFAULT '',
    owner TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	mime TEXT NOT NULL DEFAULT '',
	data BLOB,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    author TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
	last_seen TEXT NOT NULL,
	password_hash TEXT NOT NULL DEFAULT '',
	is_admin INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
	token TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	expires_at TEXT NOT NULL,
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS vulnerabilities (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	description TEXT NOT NULL,
	severity TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'open',
	owner TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	priority TEXT NOT NULL DEFAULT 'medium',
	status TEXT NOT NULL DEFAULT 'todo',
	assignee_id INTEGER,
	group_role TEXT NOT NULL DEFAULT '',
	created_by INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	FOREIGN KEY(assignee_id) REFERENCES users(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS folders (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	parent_id INTEGER,
	owner_id INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	FOREIGN KEY(parent_id) REFERENCES folders(id) ON DELETE CASCADE,
	FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS file_links (
	file_id INTEGER PRIMARY KEY,
	folder_id INTEGER NOT NULL,
	FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE,
	FOREIGN KEY(folder_id) REFERENCES folders(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS folder_access (
	folder_id INTEGER NOT NULL,
	user_id INTEGER,
	role TEXT NOT NULL DEFAULT '',
	permission TEXT NOT NULL DEFAULT 'viewer',
	PRIMARY KEY(folder_id, user_id, role),
	FOREIGN KEY(folder_id) REFERENCES folders(id) ON DELETE CASCADE,
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS chats (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	created_by INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	FOREIGN KEY(created_by) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS chat_members (
	chat_id INTEGER NOT NULL,
	user_id INTEGER NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(chat_id, user_id),
	FOREIGN KEY(chat_id) REFERENCES chats(id) ON DELETE CASCADE,
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS chat_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_id INTEGER NOT NULL,
	author_id INTEGER NOT NULL,
	text TEXT NOT NULL,
	created_at TEXT NOT NULL,
	FOREIGN KEY(chat_id) REFERENCES chats(id) ON DELETE CASCADE,
	FOREIGN KEY(author_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS department_items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	role TEXT NOT NULL,
	kind TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'open',
	due_at TEXT NOT NULL DEFAULT '',
	amount INTEGER NOT NULL DEFAULT 0,
	created_by INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	FOREIGN KEY(created_by) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_contacts_status ON contacts(status);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_events(created_at);
CREATE INDEX IF NOT EXISTS idx_department_items_role ON department_items(role, status, updated_at);
`)
	if err != nil {
		return err
	}
	if err = ensureFileColumns(db); err != nil {
		return err
	}
	if err = ensureUserColumns(db); err != nil {
		return err
	}
	return seed(db, filepath.Join(filepath.Dir(path), "employee-accounts.txt"))
}

func ensureUserColumns(db *sql.DB) error {
	columns := []string{"password_hash TEXT NOT NULL DEFAULT ''", "is_admin INTEGER NOT NULL DEFAULT 0"}
	for _, column := range columns {
		name := strings.Split(column, " ")[0]
		var exists bool
		rows, err := db.Query(`PRAGMA table_info(users)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var current, kind string
			var defaultValue sql.NullString
			if err = rows.Scan(&cid, &current, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				return err
			}
			if current == name {
				exists = true
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if !exists {
			if _, err = db.Exec(`ALTER TABLE users ADD COLUMN ` + column); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureFileColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(files)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "content" {
			continue
		}
		if name == "mime" {
			continue
		}
		if name == "data" {
			continue
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	columns := []string{"content TEXT NOT NULL DEFAULT ''", "mime TEXT NOT NULL DEFAULT ''", "data BLOB"}
	for _, column := range columns {
		name := strings.Split(column, " ")[0]
		var exists bool
		rows, err := db.Query(`PRAGMA table_info(files)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var current, kind string
			var defaultValue sql.NullString
			if err = rows.Scan(&cid, &current, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				return err
			}
			if current == name {
				exists = true
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if !exists {
			if _, err = db.Exec(`ALTER TABLE files ADD COLUMN ` + column); err != nil {
				return err
			}
		}
	}
	return nil
}

func seed(db *sql.DB, credentialPath string) error {
	if _, err := db.Exec(`INSERT INTO contacts (name, company, email, status, value, created_at)
SELECT ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM contacts)`, "Анна Смирнова", "Northstar Studio", "anna@northstar.studio", "active", 12800, "2026-09-22T12:00:00Z"); err != nil {
		return err
	}
	_, err := db.Exec(`
INSERT INTO contacts (name, company, email, status, value, created_at)
SELECT ?, ?, ?, ?, ?, ? WHERE (SELECT COUNT(*) FROM contacts) = 1`, "Михаил Орлов", "Orbital Systems", "mikhail@orbital.systems", "lead", 8400, "2026-09-23T12:00:00Z")
	if err != nil {
		return err
	}
	_, err = db.Exec(`
INSERT INTO contacts (name, company, email, status, value, created_at)
SELECT ?, ?, ?, ?, ?, ? WHERE (SELECT COUNT(*) FROM contacts) = 2`, "Елена Волкова", "Forma Finance", "elena@forma.finance", "active", 22100, "2026-09-24T04:00:00Z")
	if err != nil {
		return err
	}
	if err = seedFiles(db); err != nil {
		return err
	}
	if err = seedMessages(db); err != nil {
		return err
	}
	if err = seedAdmin(db); err != nil {
		return err
	}
	if err = seedDepartmentEmployees(db, credentialPath); err != nil {
		return err
	}
	if err = seedVulnerabilities(db); err != nil {
		return err
	}
	if err = seedTasks(db); err != nil {
		return err
	}
	if err = seedDepartmentItems(db); err != nil {
		return err
	}
	return seedCollaboration(db)
}

func seedDepartmentEmployees(db *sql.DB, credentialPath string) error {
	employees := []struct{ name, email, role string }{
		{"Елена Волкова", "elena@northstar.local", "Кадры"},
		{"Дмитрий Соколов", "dmitry@northstar.local", "Разработка"},
		{"Игорь Крылов", "igor@northstar.local", "Информационная безопасность"},
		{"Мария Орлова", "maria@northstar.local", "Бухгалтерия"},
		{"Ольга Миронова", "olga@northstar.local", "Маркетинг"},
		{"Анна Смирнова", "anna@northstar.local", "Продажи"},
	}
	var credentialFile *os.File
	defer func() {
		if credentialFile != nil {
			_ = credentialFile.Close()
		}
	}()

	for _, employee := range employees {
		var id int
		var passwordHash string
		err := db.QueryRow(`SELECT id, password_hash FROM users WHERE lower(email) = lower(?)`, employee.email).Scan(&id, &passwordHash)
		newAccount := err == sql.ErrNoRows
		if err != nil && !newAccount {
			return fmt.Errorf("find department employee %s: %w", employee.email, err)
		}

		rotatePassword := newAccount || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("Northstar123!")) == nil
		if !rotatePassword {
			continue
		}
		randomPassword := make([]byte, 24)
		if _, err = rand.Read(randomPassword); err != nil {
			return fmt.Errorf("generate password for %s: %w", employee.email, err)
		}
		password := base64.RawURLEncoding.EncodeToString(randomPassword)
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password for %s: %w", employee.email, err)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if newAccount {
			_, err = db.Exec(`INSERT INTO users (name, email, role, status, last_seen, password_hash, is_admin) VALUES (?, ?, ?, 'active', ?, ?, 0)`, employee.name, employee.email, employee.role, now, string(hash))
		} else {
			_, err = db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
		}
		if err != nil {
			return fmt.Errorf("save department employee %s: %w", employee.email, err)
		}
		if !newAccount {
			if _, err = db.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
				return fmt.Errorf("invalidate sessions for %s: %w", employee.email, err)
			}
		}

		if credentialFile == nil {
			if err = os.MkdirAll(filepath.Dir(credentialPath), 0o700); err != nil {
				return fmt.Errorf("create credential directory: %w", err)
			}
			credentialFile, err = os.OpenFile(credentialPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return fmt.Errorf("open employee credentials file: %w", err)
			}
			if err = credentialFile.Chmod(0o600); err != nil {
				return fmt.Errorf("secure employee credentials file: %w", err)
			}
		}
		if _, err = fmt.Fprintf(credentialFile, "Должность: %s\nСотрудник: %s\nEmail: %s\nПервоначальный пароль: %s\n\n", employee.role, employee.name, employee.email, password); err != nil {
			return fmt.Errorf("write employee credentials: %w", err)
		}
	}
	if credentialFile != nil {
		if err := credentialFile.Sync(); err != nil {
			return fmt.Errorf("flush employee credentials: %w", err)
		}
		if err := credentialFile.Close(); err != nil {
			return fmt.Errorf("close employee credentials: %w", err)
		}
		credentialFile = nil
	}
	return nil
}

func seedDepartmentItems(db *sql.DB) error {
	items := []struct{ role, kind, title, description string }{
		{"Кадры", "Адаптация", "Подготовить план адаптации нового сотрудника", "Согласовать доступы, встречи с командой и контрольные точки на первый месяц."},
		{"Разработка", "Релиз", "Проверить задачи перед ближайшим релизом", "Сверить готовность изменений и результаты проверок."},
		{"Информационная безопасность", "Проверка", "Проверить доступы к рабочим материалам", "Зафиксировать результат проверки и передать найденные риски в реестр уязвимостей."},
		{"Бухгалтерия", "Платёж", "Сверить реестр оплат за текущий период", "Проверить суммы и отметить платежи, требующие подтверждения."},
		{"Маркетинг", "Кампания", "Подготовить план следующей кампании", "Зафиксировать аудиторию, каналы, бюджет и критерии результата."},
		{"Продажи", "Задача", "Связаться с лидами и обновить следующие шаги", "Зафиксировать итоги контакта и ожидаемую дату продолжения сделки."},
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range items {
		_, err := db.Exec(`INSERT INTO department_items (role, kind, title, description, status, created_by, created_at, updated_at)
			SELECT ?, ?, ?, ?, 'open', id, ?, ? FROM users WHERE email = 'alexey@northstar.local'
			AND NOT EXISTS (SELECT 1 FROM department_items WHERE role = ? AND title = ?)`,
			item.role, item.kind, item.title, item.description, now, now, item.role, item.title)
		if err != nil {
			return fmt.Errorf("seed %s work item: %w", item.role, err)
		}
	}
	return nil
}

func seedCollaboration(db *sql.DB) error {
	var folderID int
	err := db.QueryRow(`SELECT id FROM folders WHERE parent_id IS NULL ORDER BY id LIMIT 1`).Scan(&folderID)
	if err == sql.ErrNoRows {
		result, createErr := db.Exec(`INSERT INTO folders (name, parent_id, owner_id, created_at) VALUES (?, NULL, ?, ?)`, "Общий диск", 1, "2026-09-24T10:00:00Z")
		if createErr != nil {
			return createErr
		}
		value, idErr := result.LastInsertId()
		if idErr != nil {
			return idErr
		}
		folderID = int(value)
	} else if err != nil {
		return err
	}
	if _, err = db.Exec(`INSERT OR IGNORE INTO file_links (file_id, folder_id) SELECT id, ? FROM files`, folderID); err != nil {
		return err
	}
	var chatCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&chatCount); err != nil {
		return err
	}
	if chatCount == 0 {
		result, createErr := db.Exec(`INSERT INTO chats (name, created_by, created_at) VALUES (?, ?, ?)`, "Общий канал", 1, "2026-09-24T10:00:00Z")
		if createErr != nil {
			return createErr
		}
		chatID, idErr := result.LastInsertId()
		if idErr != nil {
			return idErr
		}
		_, err = db.Exec(`INSERT INTO chat_members (chat_id, user_id, is_admin) SELECT ?, id, CASE WHEN id = 1 THEN 1 ELSE 0 END FROM users WHERE status = 'active'`, chatID)
	}
	return err
}

func seedTasks(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count); err != nil || count > 0 {
		return err
	}
	now := "2026-09-24T10:00:00Z"
	_, err := db.Exec(`INSERT INTO tasks (title, description, priority, status, group_role, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "Проверить доступы к общему диску", "Провести контроль прав и зафиксировать результат в аудите.", "high", "todo", "Информационная безопасность", 1, now, now)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO tasks (title, description, priority, status, group_role, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "Подготовить прогноз продаж", "Обновить прогноз по текущей воронке до конца недели.", "medium", "todo", "Продажи", 1, now, now)
	return err
}

func seedVulnerabilities(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM vulnerabilities`).Scan(&count); err != nil || count > 0 {
		return err
	}
	now := "2026-09-24T10:00:00Z"
	_, err := db.Exec(`INSERT INTO vulnerabilities (title, description, severity, status, owner, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "Недостаточная защита сессии", "Проверить срок жизни cookie и принудительную инвалидацию сессий после выхода.", "high", "open", "Игорь Крылов", now, now)
	return err
}

func seedFiles(db *sql.DB) error {
	seed := []struct{ name, kind, size, owner, content, updated string }{
		{"Презентация Northstar CRM", "PPTX", "4,8 МБ", "Анна Смирнова", "Northstar CRM\n\nПлан презентации продукта, рынка и команды.", "2026-09-24T10:00:00Z"},
		{"Политика обработки данных", "PDF", "1,2 МБ", "Отдел ИБ", "Политика обработки данных\n\nВерсия: 2.1\nВладелец: отдел информационной безопасности.", "2026-09-24T07:00:00Z"},
		{"Реестр оплат за сентябрь", "XLSX", "860 КБ", "Бухгалтерия", "Контрагент | Сумма | Статус\nNorthstar Studio | 12800 | Оплачено\nOrbital Systems | 8400 | Ожидает оплаты", "2026-09-23T10:00:00Z"},
		{"cmd/server/main.go", "GO", "3 КБ", "Разработка", "package main\n\nfunc main() {\n\t// CRM server entrypoint\n}", "2026-09-24T09:45:00Z"},
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&count); err != nil || count > 0 {
		if err != nil {
			return err
		}
		_, err = db.Exec(`UPDATE files SET content = CASE name
			WHEN 'Презентация Northstar CRM' THEN ?
			WHEN 'Политика обработки данных' THEN ?
			WHEN 'Реестр оплат за сентябрь' THEN ?
			WHEN 'cmd/server/main.go' THEN ?
			ELSE content END WHERE content = ''`, seed[0].content, seed[1].content, seed[2].content, seed[3].content)
		if err != nil {
			return err
		}
		_, err = db.Exec(`INSERT INTO files (name, kind, size, owner, content, updated_at) SELECT ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM files WHERE name = ?)`, seed[3].name, seed[3].kind, seed[3].size, seed[3].owner, seed[3].content, seed[3].updated, seed[3].name)
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, file := range seed {
		if _, err = tx.Exec(`INSERT INTO files (name, kind, size, owner, content, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, file.name, file.kind, file.size, file.owner, file.content, file.updated); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func seedMessages(db *sql.DB) error {
	seed := []struct{ author, role, text, created string }{
		{"Анна Смирнова", "Продажи", "Клиент Northstar запросил обновлённое предложение.", "2026-09-24T10:00:00Z"},
		{"Игорь Крылов", "ИБ", "Проверка доступа к общему диску завершена.", "2026-09-24T09:30:00Z"},
		{"Мария Орлова", "Бухгалтерия", "Реестр оплат за сентябрь загружен.", "2026-09-24T08:00:00Z"},
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count); err != nil || count > 0 {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, message := range seed {
		if _, err = tx.Exec(`INSERT INTO messages (author, role, text, created_at) VALUES (?, ?, ?, ?)`, message.author, message.role, message.text, message.created); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func seedAdmin(db *sql.DB) error {
	seedUsers := []struct{ name, email, role, status, lastSeen string }{
		{"Алексей Крылов", "alexey@northstar.local", "Менеджмент", "active", "2026-09-24T10:00:00Z"},
		{"Дмитрий Соколов", "dmitry@northstar.local", "Разработка", "active", "2026-09-24T09:45:00Z"},
		{"Игорь Крылов", "igor@northstar.local", "Информационная безопасность", "active", "2026-09-24T09:20:00Z"},
		{"Мария Орлова", "maria@northstar.local", "Бухгалтерия", "active", "2026-09-24T08:00:00Z"},
		{"Анна Смирнова", "anna@northstar.local", "Продажи", "inactive", "2026-09-21T10:00:00Z"},
	}
	var count int
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Northstar123!"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil || count > 0 {
		if err != nil {
			return err
		}
		_, err = db.Exec(`UPDATE users SET password_hash = ?, is_admin = CASE WHEN email = 'alexey@northstar.local' THEN 1 ELSE is_admin END WHERE password_hash = ''`, string(passwordHash))
		if err != nil {
			return err
		}
		_, err = db.Exec(`UPDATE users SET status = 'active' WHERE email = 'anna@northstar.local'`)
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, user := range seedUsers {
		isAdmin := 0
		if user.email == "alexey@northstar.local" {
			isAdmin = 1
		}
		if _, err = tx.Exec(`INSERT INTO users (name, email, role, status, last_seen, password_hash, is_admin) VALUES (?, ?, ?, ?, ?, ?, ?)`, user.name, user.email, user.role, user.status, user.lastSeen, string(passwordHash), isAdmin); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO audit_events (actor, action, target, created_at) VALUES (?, ?, ?, ?)`, "Алексей Крылов", "Система инициализирована", "CRM", "2026-09-24T10:00:00Z"); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.DB.Close() }
