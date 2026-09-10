package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiagnosticsRequiresGET(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/v1/diagnostics", nil)
	w := httptest.NewRecorder()
	s.diagnostics(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestDiagnosticsRequiresControlHeader(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodGet, "/v1/diagnostics", nil)
	w := httptest.NewRecorder()
	s.diagnostics(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}
