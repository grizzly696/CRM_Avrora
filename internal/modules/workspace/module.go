package workspace

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type File struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Size      string    `json:"size"`
	Owner     string    `json:"owner"`
	Content   string    `json:"content,omitempty"`
	Mime      string    `json:"mime,omitempty"`
	HasBinary bool      `json:"hasBinary"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type Message struct {
	ID        int       `json:"id"`
	Author    string    `json:"author"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}
type Module struct {
	db   *sql.DB
	auth *auth.Service
}

func New(db *sql.DB, authService *auth.Service) *Module {
	return &Module{db: db, auth: authService}
}
func (m *Module) Name() string { return "workspace" }
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspace/files", m.listFiles)
	mux.HandleFunc("POST /api/workspace/files", m.createFile)
	mux.HandleFunc("POST /api/workspace/files/upload", m.uploadFile)
	mux.HandleFunc("GET /api/workspace/files/{id}", m.getFile)
	mux.HandleFunc("PATCH /api/workspace/files/{id}", m.updateFile)
	mux.HandleFunc("GET /api/workspace/files/{id}/download", m.downloadFile)
	mux.HandleFunc("GET /api/workspace/messages", m.listMessages)
	mux.HandleFunc("POST /api/workspace/messages", m.createMessage)
}

func (m *Module) listFiles(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(`SELECT id, name, kind, size, owner, updated_at FROM files ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
		return
	}
	defer rows.Close()
	files := make([]File, 0)
	for rows.Next() {
		var file File
		var updated string
		if err = rows.Scan(&file.ID, &file.Name, &file.Kind, &file.Size, &file.Owner, &updated); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read files"})
			return
		}
		file.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid file date"})
			return
		}
		files = append(files, file)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read files"})
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (m *Module) getFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var file File
	var updated string
	var data []byte
	err := m.db.QueryRow(`SELECT id, name, kind, size, owner, content, mime, data, updated_at FROM files WHERE id = ?`, id).Scan(&file.ID, &file.Name, &file.Kind, &file.Size, &file.Owner, &file.Content, &file.Mime, &data, &updated)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	file.HasBinary = len(data) > 0
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file"})
		return
	}
	file.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid file date"})
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (m *Module) downloadFile(w http.ResponseWriter, r *http.Request) {
	var name, mime string
	var data []byte
	err := m.db.QueryRow(`SELECT name, mime, data FROM files WHERE id = ?`, r.PathValue("id")).Scan(&name, &mime, &data)
	if err == sql.ErrNoRows {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}
	if len(data) == 0 {
		data = []byte{}
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(name)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (m *Module) createFile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Size    string `json:"size"`
		Owner   string `json:"owner"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	input.Name, input.Kind, input.Size, input.Owner = strings.TrimSpace(input.Name), strings.ToUpper(strings.TrimSpace(input.Kind)), strings.TrimSpace(input.Size), strings.TrimSpace(input.Owner)
	if input.Kind == "" {
		input.Kind = "FILE"
	}
	updated := time.Now().UTC()
	result, err := m.db.Exec(`INSERT INTO files (name, kind, size, owner, content, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, input.Name, input.Kind, input.Size, input.Owner, input.Content, updated.Format(time.RFC3339Nano))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create file"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create file"})
		return
	}
	writeJSON(w, http.StatusCreated, File{ID: int(id), Name: input.Name, Kind: input.Kind, Size: input.Size, Owner: input.Owner, UpdatedAt: updated})
}

func (m *Module) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is too large or invalid"})
		return
	}
	_, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
		return
	}
	file, err := header.Open()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to open file"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read file"})
		return
	}
	name := filepath.Base(header.Filename)
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" {
		ext = "FILE"
	}
	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}
	preview := extractPreview(name, mime, data)
	owner := strings.TrimSpace(r.FormValue("owner"))
	if owner == "" {
		owner = "Команда"
	}
	updated := time.Now().UTC()
	result, err := m.db.Exec(`INSERT INTO files (name, kind, size, owner, content, mime, data, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, name, ext, formatBytes(len(data)), owner, preview, mime, data, updated.Format(time.RFC3339Nano))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
		return
	}
	writeJSON(w, http.StatusCreated, File{ID: int(id), Name: name, Kind: ext, Size: formatBytes(len(data)), Owner: owner, Content: preview, Mime: mime, HasBinary: true, UpdatedAt: updated})
}

func (m *Module) updateFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := m.db.Exec(`UPDATE files SET name = ?, content = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(input.Name), input.Content, updated, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update file"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	m.getFile(w, r)
}

func (m *Module) listMessages(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(`SELECT id, author, role, text, created_at FROM messages ORDER BY created_at ASC, id ASC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list messages"})
		return
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var created string
		if err = rows.Scan(&message.ID, &message.Author, &message.Role, &message.Text, &created); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read messages"})
			return
		}
		message.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid message date"})
			return
		}
		messages = append(messages, message)
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read messages"})
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func (m *Module) createMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := m.auth.User(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	created := time.Now().UTC()
	result, err := m.db.Exec(`INSERT INTO messages (author, role, text, created_at) VALUES (?, ?, ?, ?)`, user.Name, user.Role, input.Text, created.Format(time.RFC3339Nano))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create message"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create message"})
		return
	}
	writeJSON(w, http.StatusCreated, Message{ID: int(id), Author: input.Author, Role: input.Role, Text: input.Text, CreatedAt: created})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func extractPreview(name, mime string, data []byte) string {
	if utf8.Valid(data) && !strings.Contains(mime, "pdf") && !strings.Contains(mime, "zip") {
		return string(data)
	}
	if strings.EqualFold(filepath.Ext(name), ".pdf") {
		return "PDF-документ сохранён. Скачайте оригинал для полного просмотра."
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "Бинарный файл сохранён. Скачайте оригинал для просмотра."
	}
	var output strings.Builder
	for _, file := range reader.File {
		if !strings.HasSuffix(file.Name, ".xml") {
			continue
		}
		if !strings.Contains(file.Name, "word/document") && !strings.Contains(file.Name, "sharedStrings") && !strings.Contains(file.Name, "sheet") && !strings.Contains(file.Name, "slides/slide") {
			continue
		}
		opened, openErr := file.Open()
		if openErr != nil {
			continue
		}
		decoder := xml.NewDecoder(opened)
		for {
			token, tokenErr := decoder.Token()
			if tokenErr == io.EOF {
				break
			}
			if tokenErr != nil {
				break
			}
			if text, ok := token.(xml.CharData); ok {
				value := strings.TrimSpace(string(text))
				if value != "" {
					output.WriteString(value)
					output.WriteString(" ")
				}
			}
		}
		opened.Close()
	}
	preview := strings.TrimSpace(output.String())
	if preview == "" {
		return "Офисный документ сохранён. Скачайте оригинал для полного просмотра."
	}
	return preview
}

func formatBytes(size int) string {
	if size < 1024 {
		return strconv.Itoa(size) + " Б"
	}
	if size < 1024*1024 {
		return strconv.Itoa(size/1024) + " КБ"
	}
	return strconv.Itoa(size/(1024*1024)) + " МБ"
}
