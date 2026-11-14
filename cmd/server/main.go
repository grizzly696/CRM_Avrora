package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"time"

	"crm/internal/auth"
	"crm/internal/modules/admin"
	"crm/internal/modules/collaboration"
	"crm/internal/modules/contacts"
	"crm/internal/modules/departments"
	securitymodule "crm/internal/modules/security"
	tasksmodule "crm/internal/modules/tasks"
	"crm/internal/modules/workspace"
	"crm/internal/platform"
	"crm/internal/storage"
)

//go:embed web
var webFiles embed.FS

func main() {
	store, err := storage.Open("data/crm.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	mux := http.NewServeMux()
	authService := auth.New(store.DB)
	authService.Routes(mux)
	modules := []platform.Module{admin.New(store.DB, authService), contacts.New(store.DB), workspace.New(store.DB, authService), securitymodule.New(store.DB, authService), tasksmodule.New(store.DB, authService), departments.New(store.DB, authService), collaboration.New(store.DB, authService)}
	for _, module := range modules {
		module.Routes(mux)
		log.Printf("module loaded: %s", module.Name())
	}

	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(static)))

	server := &http.Server{Addr: ":8080", Handler: logging(authService.Middleware(mux)), ReadHeaderTimeout: 5 * time.Second}
	log.Println("CRM running at http://localhost:8080")
	log.Fatal(server.ListenAndServe())
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Microsecond))
	})
}
