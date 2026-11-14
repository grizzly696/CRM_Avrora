package collaboration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crm/internal/auth"
)

type Module struct {
	db   *sql.DB
	auth *auth.Service
}

func New(db *sql.DB, authService *auth.Service) *Module { return &Module{db: db, auth: authService} }
func (m *Module) Name() string                          { return "collaboration" }
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/drive", m.drive)
	mux.HandleFunc("POST /api/drive/folders", m.createFolder)
	mux.HandleFunc("POST /api/drive/folders/{id}/files", m.uploadFile)
	mux.HandleFunc("POST /api/drive/folders/{id}/documents", m.createDocument)
	mux.HandleFunc("GET /api/drive/files/{id}", m.getFile)
	mux.HandleFunc("PATCH /api/drive/files/{id}", m.updateFile)
	mux.HandleFunc("GET /api/drive/files/{id}/download", m.downloadFile)
	mux.HandleFunc("POST /api/drive/folders/{id}/access", m.grantFolderAccess)
	mux.HandleFunc("GET /api/drive/folders/{id}/access", m.listFolderAccess)
	mux.HandleFunc("GET /api/chats", m.listChats)
	mux.HandleFunc("POST /api/chats", m.createChat)
	mux.HandleFunc("POST /api/chats/{id}/members", m.addChatMember)
	mux.HandleFunc("GET /api/chats/{id}/messages", m.listChatMessages)
	mux.HandleFunc("POST /api/chats/{id}/messages", m.createChatMessage)
}

func (m *Module) getFile(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	var fileID, folderID int
	var name, kind, size, owner, content, mime, updated string
	err := m.db.QueryRow(`SELECT f.id, l.folder_id, f.name, f.kind, f.size, f.owner, f.content, f.mime, f.updated_at FROM files f JOIN file_links l ON l.file_id = f.id WHERE f.id = ?`, r.PathValue("id")).Scan(&fileID, &folderID, &name, &kind, &size, &owner, &content, &mime, &updated)
	if err == sql.ErrNoRows {
		writeJSON(w, 404, map[string]string{"error": "file not found"})
		return
	}
	if err != nil || !m.canAccessFolder(user, folderID) {
		writeJSON(w, 403, map[string]string{"error": "file access denied"})
		return
	}
	parsed, _ := time.Parse(time.RFC3339Nano, updated)
	writeJSON(w, 200, map[string]any{"id": fileID, "name": name, "kind": kind, "size": size, "owner": owner, "content": content, "mime": mime, "updatedAt": parsed, "folderId": folderID})
}

func (m *Module) updateFile(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	var folderID int
	if err := m.db.QueryRow(`SELECT folder_id FROM file_links WHERE file_id = ?`, r.PathValue("id")).Scan(&folderID); err != nil || !m.canEditFolder(user, folderID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "file edit denied"})
		return
	}
	var input struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeJSON(w, 400, map[string]string{"error": "name is required"})
		return
	}
	content := []byte(input.Content)
	_, err := m.db.Exec(`UPDATE files SET name = ?, content = ?, data = ?, size = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(input.Name), input.Content, content, formatBytes(len(content)), time.Now().UTC().Format(time.RFC3339Nano), r.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to update file"})
		return
	}
	writeJSON(w, 200, map[string]string{"message": "file updated"})
}

func (m *Module) drive(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	parent := 0
	if value := r.URL.Query().Get("folderId"); value != "" {
		parent, _ = strconv.Atoi(value)
	}
	folderID := parent
	if folderID == 0 {
		if err := m.db.QueryRow(`SELECT id FROM folders WHERE parent_id IS NULL ORDER BY id LIMIT 1`).Scan(&folderID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "root folder is not initialized"})
			return
		}
	}
	if !m.canAccessFolder(user, folderID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "folder access denied"})
		return
	}
	var folderOwner int
	if err := m.db.QueryRow(`SELECT owner_id FROM folders WHERE id = ?`, folderID).Scan(&folderOwner); err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "folder not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read folder"})
		return
	}
	rows, err := m.db.Query(`SELECT id, name, owner_id FROM folders WHERE parent_id IS ? ORDER BY name`, nullableParent(folderID))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to list folders"})
		return
	}
	defer rows.Close()
	folders := make([]map[string]any, 0)
	for rows.Next() {
		var id, owner int
		var name string
		if err = rows.Scan(&id, &name, &owner); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to read folders"})
			return
		}
		if user.IsAdmin || m.canAccessFolder(user, id) {
			folders = append(folders, map[string]any{"id": id, "name": name, "ownerId": owner})
		}
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to read folders"})
		return
	}
	filesRows, err := m.db.Query(`SELECT f.id, f.name, f.kind, f.size, f.owner, f.updated_at FROM files f JOIN file_links l ON l.file_id = f.id WHERE l.folder_id = ? ORDER BY f.name`, folderID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to list files"})
		return
	}
	defer filesRows.Close()
	files := make([]map[string]any, 0)
	for filesRows.Next() {
		var id int
		var name, kind, size, owner, updated string
		if err = filesRows.Scan(&id, &name, &kind, &size, &owner, &updated); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to read files"})
			return
		}
		parsed, _ := time.Parse(time.RFC3339Nano, updated)
		files = append(files, map[string]any{"id": id, "name": name, "kind": kind, "size": size, "owner": owner, "updatedAt": parsed})
	}
	if err = filesRows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to read files"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"folderId": folderID, "folders": folders, "files": files,
		"canManageAccess": user.IsAdmin || user.ID == folderOwner,
	})
}

func (m *Module) createFolder(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	var input struct {
		Name     string `json:"name"`
		ParentID int    `json:"parentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeJSON(w, 400, map[string]string{"error": "name is required"})
		return
	}
	if input.ParentID > 0 && !m.canAccessFolder(user, input.ParentID) {
		writeJSON(w, 403, map[string]string{"error": "folder access denied"})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := m.db.Exec(`INSERT INTO folders (name, parent_id, owner_id, created_at) VALUES (?, NULLIF(?, 0), ?, ?)`, strings.TrimSpace(input.Name), input.ParentID, user.ID, now)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create folder"})
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, 201, map[string]any{"id": id, "name": strings.TrimSpace(input.Name), "parentId": input.ParentID})
}

func (m *Module) uploadFile(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	folderID, _ := strconv.Atoi(r.PathValue("id"))
	if !m.canAccessFolder(user, folderID) {
		writeJSON(w, 403, map[string]string{"error": "folder access denied"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid file"})
		return
	}
	_, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "file is required"})
		return
	}
	file, err := header.Open()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "failed to open file"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "failed to read file"})
		return
	}
	name := filepath.Base(header.Filename)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := m.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to store file"})
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO files (name, kind, size, owner, content, mime, data, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, name, strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), ".")), formatBytes(len(data)), user.Name, "", header.Header.Get("Content-Type"), data, now)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to store file"})
		return
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to store file"})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO file_links (file_id, folder_id) VALUES (?, ?)`, fileID, folderID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to link file"})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to store file"})
		return
	}
	writeJSON(w, 201, map[string]any{"id": fileID, "name": name, "folderId": folderID})
}

func (m *Module) createDocument(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	folderID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || folderID < 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "folder edit denied"})
		return
	}
	var folderExists int
	if err = m.db.QueryRow(`SELECT COUNT(*) FROM folders WHERE id = ?`, folderID).Scan(&folderExists); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to validate folder"})
		return
	}
	if folderExists == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "folder not found"})
		return
	}
	if !m.canEditFolder(user, folderID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "folder edit denied"})
		return
	}
	var input struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "document name and valid content are required"})
		return
	}
	name := filepath.Base(strings.TrimSpace(input.Name))
	if name == "." || name == string(filepath.Separator) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid document name"})
		return
	}
	kind := strings.ToUpper(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
	}
	if kind == "" {
		kind = "FILE"
	}
	content := []byte(input.Content)
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := m.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create document"})
		return
	}
	result, err := tx.Exec(`INSERT INTO files (name, kind, size, owner, content, mime, data, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, kind, formatBytes(len(content)), user.Name, input.Content, contentType, content, now)
	if err != nil {
		_ = tx.Rollback()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create document"})
		return
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create document"})
		return
	}
	if _, err = tx.Exec(`INSERT INTO file_links (file_id, folder_id) VALUES (?, ?)`, fileID, folderID); err != nil {
		_ = tx.Rollback()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to link document"})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save document"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": fileID, "name": name, "folderId": folderID})
}

func (m *Module) downloadFile(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	id := r.PathValue("id")
	var name, mime string
	var data []byte
	var folder int
	err := m.db.QueryRow(`SELECT f.name, f.mime, f.data, l.folder_id FROM files f JOIN file_links l ON l.file_id = f.id WHERE f.id = ?`, id).Scan(&name, &mime, &data, &folder)
	if err == sql.ErrNoRows {
		http.Error(w, "file not found", 404)
		return
	}
	if err != nil || !m.canAccessFolder(user, folder) {
		http.Error(w, "file access denied", 403)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(name)+`"`)
	_, _ = w.Write(data)
}

func (m *Module) grantFolderAccess(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	folderID, _ := strconv.Atoi(r.PathValue("id"))
	if !m.isFolderOwner(user, folderID) {
		writeJSON(w, 403, map[string]string{"error": "only folder owner or admin can change access"})
		return
	}
	var input struct {
		UserID     int    `json:"userId"`
		Role       string `json:"role"`
		Permission string `json:"permission"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || (input.UserID == 0 && strings.TrimSpace(input.Role) == "") {
		writeJSON(w, 400, map[string]string{"error": "userId or role is required"})
		return
	}
	if input.Permission == "" {
		input.Permission = "viewer"
	}
	_, err := m.db.Exec(`INSERT OR REPLACE INTO folder_access (folder_id, user_id, role, permission) VALUES (NULLIF(?, 0), NULLIF(?, 0), ?, ?)`, folderID, input.UserID, strings.TrimSpace(input.Role), input.Permission)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to grant access"})
		return
	}
	writeJSON(w, 201, map[string]string{"message": "access updated"})
}
func (m *Module) listFolderAccess(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	folderID, _ := strconv.Atoi(r.PathValue("id"))
	if !m.canAccessFolder(user, folderID) {
		writeJSON(w, 403, map[string]string{"error": "folder access denied"})
		return
	}
	rows, err := m.db.Query(`SELECT a.user_id, COALESCE(u.name, ''), a.role, a.permission FROM folder_access a LEFT JOIN users u ON u.id = a.user_id WHERE a.folder_id = ?`, folderID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to list access"})
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id int
		var name, role, permission string
		if rows.Scan(&id, &name, &role, &permission) == nil {
			result = append(result, map[string]any{"userId": id, "name": name, "role": role, "permission": permission})
		}
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to read access"})
		return
	}
	writeJSON(w, 200, result)
}

func (m *Module) listChats(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	rows, err := m.db.Query(`SELECT c.id, c.name, c.created_by, COALESCE((SELECT text FROM chat_messages WHERE chat_id = c.id ORDER BY id DESC LIMIT 1), '') FROM chats c JOIN chat_members cm ON cm.chat_id = c.id WHERE cm.user_id = ? ORDER BY c.name`, user.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to list chats"})
		return
	}
	defer rows.Close()
	chats := make([]map[string]any, 0)
	for rows.Next() {
		var id, creator int
		var name, last string
		if rows.Scan(&id, &name, &creator, &last) == nil {
			chats = append(chats, map[string]any{"id": id, "name": name, "createdBy": creator, "lastMessage": last})
		}
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to read chats"})
		return
	}
	usersRows, _ := m.db.Query(`SELECT id, name, role FROM users WHERE status = 'active' ORDER BY name`)
	users := make([]map[string]any, 0)
	if usersRows != nil {
		defer usersRows.Close()
		for usersRows.Next() {
			var id int
			var name, role string
			if usersRows.Scan(&id, &name, &role) == nil {
				users = append(users, map[string]any{"id": id, "name": name, "role": role})
			}
		}
		if err = usersRows.Err(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to read users"})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"chats": chats, "users": users})
}
func (m *Module) createChat(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	var input struct {
		Name      string `json:"name"`
		MemberIDs []int  `json:"memberIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeJSON(w, 400, map[string]string{"error": "name is required"})
		return
	}
	name := strings.TrimSpace(input.Name)
	if len(name) > 120 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat name is too long"})
		return
	}
	members := make(map[int]struct{}, len(input.MemberIDs)+1)
	members[user.ID] = struct{}{}
	for _, memberID := range input.MemberIDs {
		if memberID < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid member id"})
			return
		}
		members[memberID] = struct{}{}
	}
	tx, err := m.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create chat"})
		return
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(r.Context(), `INSERT INTO chats (name, created_by, created_at) VALUES (?, ?, ?)`, name, user.ID, now)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create chat"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create chat"})
		return
	}
	for memberID := range members {
		var active int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ? AND status = 'active'`, memberID).Scan(&active); err != nil || active == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat member must be an active user"})
			return
		}
		isAdmin := 0
		if memberID == user.ID {
			isAdmin = 1
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO chat_members (chat_id, user_id, is_admin) VALUES (?, ?, ?)`, id, memberID, isAdmin); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to add chat member"})
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create chat"})
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": name})
}
func (m *Module) addChatMember(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	chatID, parseErr := strconv.Atoi(r.PathValue("id"))
	if parseErr != nil || chatID < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid chat id"})
		return
	}
	if !m.isChatAdmin(user.ID, chatID) && !user.IsAdmin {
		writeJSON(w, 403, map[string]string{"error": "only chat admin can add members"})
		return
	}
	var input struct {
		UserID int `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.UserID == 0 {
		writeJSON(w, 400, map[string]string{"error": "userId is required"})
		return
	}
	var active int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ? AND status = 'active'`, input.UserID).Scan(&active); err != nil || active == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "active user not found"})
		return
	}
	result, err := m.db.Exec(`INSERT OR IGNORE INTO chat_members (chat_id, user_id) VALUES (?, ?)`, chatID, input.UserID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to add member"})
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to add member"})
		return
	}
	if changed == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user is already a chat member"})
		return
	}
	writeJSON(w, 201, map[string]string{"message": "member added"})
}
func (m *Module) listChatMessages(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	chatID, _ := strconv.Atoi(r.PathValue("id"))
	if !m.isMember(user.ID, chatID) && !user.IsAdmin {
		writeJSON(w, 403, map[string]string{"error": "chat access denied"})
		return
	}
	rows, err := m.db.Query(`SELECT m.id, u.name, u.role, m.text, m.created_at FROM chat_messages m JOIN users u ON u.id = m.author_id WHERE m.chat_id = ? ORDER BY m.id`, chatID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to list messages"})
		return
	}
	defer rows.Close()
	messages := make([]map[string]any, 0)
	for rows.Next() {
		var id int
		var author, role, text, created string
		if rows.Scan(&id, &author, &role, &text, &created) == nil {
			messages = append(messages, map[string]any{"id": id, "author": author, "role": role, "text": text, "createdAt": created})
		}
	}
	if err = rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to read messages"})
		return
	}
	writeJSON(w, 200, messages)
}
func (m *Module) createChatMessage(w http.ResponseWriter, r *http.Request) {
	user, _ := m.auth.User(r)
	chatID, _ := strconv.Atoi(r.PathValue("id"))
	if !m.isMember(user.ID, chatID) && !user.IsAdmin {
		writeJSON(w, 403, map[string]string{"error": "chat access denied"})
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Text) == "" {
		writeJSON(w, 400, map[string]string{"error": "text is required"})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := m.db.Exec(`INSERT INTO chat_messages (chat_id, author_id, text, created_at) VALUES (?, ?, ?, ?)`, chatID, user.ID, strings.TrimSpace(input.Text), now)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to send message"})
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, 201, map[string]any{"id": id, "text": strings.TrimSpace(input.Text)})
}

func (m *Module) canAccessFolder(user auth.User, folderID int) bool {
	if user.IsAdmin {
		return true
	}
	var count int
	if m.db.QueryRow(`SELECT COUNT(*) FROM folder_access WHERE folder_id = ?`, folderID).Scan(&count) != nil {
		return false
	}
	if count == 0 {
		return true
	}
	var allowed int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM folder_access WHERE folder_id = ? AND (user_id = ? OR role = ?)`, folderID, user.ID, user.Role).Scan(&allowed)
	return allowed > 0
}
func (m *Module) canEditFolder(user auth.User, folderID int) bool {
	if user.IsAdmin || m.isFolderOwner(user, folderID) {
		return true
	}
	var count int
	if m.db.QueryRow(`SELECT COUNT(*) FROM folder_access WHERE folder_id = ?`, folderID).Scan(&count) != nil {
		return false
	}
	if count == 0 {
		return true
	}
	var allowed int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM folder_access WHERE folder_id = ? AND permission = 'editor' AND (user_id = ? OR role = ?)`, folderID, user.ID, user.Role).Scan(&allowed)
	return allowed > 0
}
func (m *Module) isFolderOwner(user auth.User, folderID int) bool {
	if user.IsAdmin {
		return true
	}
	var owner int
	return m.db.QueryRow(`SELECT owner_id FROM folders WHERE id = ?`, folderID).Scan(&owner) == nil && owner == user.ID
}
func (m *Module) isMember(userID, chatID int) bool {
	var count int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM chat_members WHERE chat_id = ? AND user_id = ?`, chatID, userID).Scan(&count)
	return count > 0
}
func (m *Module) isChatAdmin(userID, chatID int) bool {
	var count int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM chat_members WHERE chat_id = ? AND user_id = ? AND is_admin = 1`, chatID, userID).Scan(&count)
	return count > 0
}
func nullableParent(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
func formatBytes(value int) string {
	if value < 1024 {
		return fmt.Sprintf("%d Б", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%d КБ", value/1024)
	}
	return fmt.Sprintf("%d МБ", value/(1024*1024))
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
