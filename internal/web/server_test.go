package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthzOK(t *testing.T) {
	h := New(Config{}, Deps{}).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Errorf("body = %v, want {\"status\":\"ok\"}", body)
	}
}

func TestHealthzRejectsNonGET(t *testing.T) {
	h := New(Config{}, Deps{}).Handler()
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/healthz", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /healthz status = %d, want 405", m, rec.Code)
		}
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	h := New(Config{}, Deps{}).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

type failingWriter struct{ header http.Header }

func (f *failingWriter) Header() http.Header       { return f.header }
func (f *failingWriter) Write([]byte) (int, error) { return 0, http.ErrHandlerTimeout }
func (f *failingWriter) WriteHeader(int)           {}

func TestWriteJSONLogsWriteError(t *testing.T) {
	var logs bytes.Buffer
	s := New(Config{}, Deps{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	s.writeJSON(&failingWriter{header: http.Header{}}, http.StatusOK, map[string]string{"status": "ok"})
	if !strings.Contains(logs.String(), "writing json response") {
		t.Errorf("write error not logged; logs = %q", logs.String())
	}
}
