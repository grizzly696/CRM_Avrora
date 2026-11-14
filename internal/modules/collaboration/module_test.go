package collaboration

import (
	"bytes"
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

func TestCreateDocumentLinksItToDriveFolder(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	var userID, folderID int
	if err := store.DB.QueryRow(`SELECT id FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1`).Scan(&userID); err != nil {
		t.Fatalf("find admin user: %v", err)
	}
	if err := store.DB.QueryRow(`SELECT id FROM folders WHERE parent_id IS NULL ORDER BY id LIMIT 1`).Scan(&folderID); err != nil {
		t.Fatalf("find root folder: %v", err)
	}
	if _, err := store.DB.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		"document-session", userID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("create test session: %v", err)
	}

	service := auth.New(store.DB)
	mux := http.NewServeMux()
	service.Routes(mux)
	New(store.DB, service).Routes(mux)
	handler := service.Middleware(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "crm_session", Value: "document-session"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	document := request(http.MethodPost, "/api/drive/folders/"+strconv.Itoa(folderID)+"/documents",
		`{"name":"release-notes.md","kind":"MD","content":"Drive document body"}`)
	if document.Code != http.StatusCreated {
		t.Fatalf("create document status = %d, want %d: %s", document.Code, http.StatusCreated, document.Body)
	}

	var result struct {
		ID       int    `json:"id"`
		FolderID int    `json:"folderId"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(document.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode created document: %v", err)
	}
	if result.ID == 0 || result.FolderID != folderID || result.Name != "release-notes.md" {
		t.Fatalf("unexpected create response: %+v", result)
	}

	var linkedFolder int
	if err := store.DB.QueryRow(`SELECT folder_id FROM file_links WHERE file_id = ?`, result.ID).Scan(&linkedFolder); err != nil {
		t.Fatalf("find document link: %v", err)
	}
	if linkedFolder != folderID {
		t.Fatalf("linked folder = %d, want %d", linkedFolder, folderID)
	}
	var content string
	if err := store.DB.QueryRow(`SELECT content FROM files WHERE id = ?`, result.ID).Scan(&content); err != nil {
		t.Fatalf("read document content: %v", err)
	}
	if content != "Drive document body" {
		t.Fatalf("document content = %q, want original content", content)
	}
	filePath := "/api/drive/files/" + strconv.Itoa(result.ID)
	download := request(http.MethodGet, filePath+"/download", "")
	if download.Code != http.StatusOK || download.Body.String() != "Drive document body" {
		t.Fatalf("download response = %d %q, want original document bytes", download.Code, download.Body.String())
	}
	updated := request(http.MethodPatch, filePath, `{"name":"release-notes.md","content":"Updated drive document"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update document status = %d, want %d: %s", updated.Code, http.StatusOK, updated.Body)
	}
	download = request(http.MethodGet, filePath+"/download", "")
	if download.Code != http.StatusOK || download.Body.String() != "Updated drive document" {
		t.Fatalf("download after edit = %d %q, want updated document bytes", download.Code, download.Body.String())
	}

	drive := request(http.MethodGet, "/api/drive?folderId="+strconv.Itoa(folderID), "")
	if drive.Code != http.StatusOK {
		t.Fatalf("list drive status = %d, want %d: %s", drive.Code, http.StatusOK, drive.Body)
	}
	var driveResult struct {
		CanManageAccess bool `json:"canManageAccess"`
	}
	if err := json.Unmarshal(drive.Body.Bytes(), &driveResult); err != nil {
		t.Fatalf("decode drive metadata: %v", err)
	}
	if !driveResult.CanManageAccess {
		t.Fatal("admin should be allowed to manage folder access")
	}

	invalid := request(http.MethodPost, "/api/drive/folders/"+strconv.Itoa(folderID)+"/documents", `{"name":"  "}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid document status = %d, want %d: %s", invalid.Code, http.StatusBadRequest, invalid.Body)
	}
	var malformedCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM files WHERE name = ?`, "").Scan(&malformedCount); err != nil {
		t.Fatalf("query malformed document count: %v", err)
	}
	if malformedCount != 0 {
		t.Fatalf("invalid document created %d unnamed files", malformedCount)
	}
}
