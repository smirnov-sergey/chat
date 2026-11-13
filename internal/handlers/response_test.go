package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	for _, tt := range []struct {
		name     string
		status   int
		response any
		wantCode int
		wantBody string
	}{
		{
			name:     "success",
			status:   http.StatusOK,
			response: healthResponse{Status: "ok"},
			wantCode: http.StatusOK,
			wantBody: "{\"status\":\"ok\"}\n",
		},
		{
			name:     "custom status and Unicode",
			status:   http.StatusCreated,
			response: map[string]string{"name": "Иван"},
			wantCode: http.StatusCreated,
			wantBody: "{\"name\":\"Иван\"}\n",
		},
		{
			name:     "marshal error",
			status:   http.StatusCreated,
			response: make(chan int),
			wantCode: http.StatusInternalServerError,
			wantBody: "{\"error\":\"internal server error\"}\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeJSON(rec, tt.status, tt.response)
			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

type failedResponseWriter struct {
	header      http.Header
	status      int
	headerCalls int
	writeCalls  int
}

func (w *failedResponseWriter) Header() http.Header {
	return w.header
}

func (w *failedResponseWriter) WriteHeader(status int) {
	w.status = status
	w.headerCalls++
}

func (w *failedResponseWriter) Write([]byte) (int, error) {
	w.writeCalls++
	return 0, errors.New("connection closed")
}

func TestWriteJSONDoesNotRetryAfterWriteError(t *testing.T) {
	w := &failedResponseWriter{header: make(http.Header)}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	if w.status != http.StatusOK || w.headerCalls != 1 || w.writeCalls != 1 {
		t.Fatalf("status = %d, header calls = %d, write calls = %d; want 200, 1, 1",
			w.status, w.headerCalls, w.writeCalls)
	}
}
