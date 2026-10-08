package httpapi

import (
	_ "embed"
	"net/http"
)

// openapiDoc is a copy of specs/001-disposable-inbox-service/contracts/openapi.yaml
// (embed cannot reach outside the package). tests/integration/openapi_test.go
// fails if the two differ.
//
//go:embed openapi.yaml
var openapiDoc []byte

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(openapiDoc)
}
