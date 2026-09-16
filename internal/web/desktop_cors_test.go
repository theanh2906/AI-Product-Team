package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDesktopCORSAllowsTauriPreflight(t *testing.T) {
	handler := (&server{}).withDesktopCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("preflight request reached the application handler")
	}))
	request := httptest.NewRequest(http.MethodOptions, "/api/settings", nil)
	request.Header.Set("Origin", "http://tauri.localhost")
	request.Header.Set("Access-Control-Request-Private-Network", "true")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://tauri.localhost" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("Access-Control-Allow-Private-Network = %q", got)
	}
}

func TestDesktopCORSDoesNotTrustArbitraryOrigins(t *testing.T) {
	handler := (&server{}).withDesktopCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected Access-Control-Allow-Origin = %q", got)
	}
}
