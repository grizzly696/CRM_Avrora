package platform

import "net/http"

// Module is the extension point for independently deployable CRM features.
type Module interface {
	Name() string
	Routes(mux *http.ServeMux)
}
