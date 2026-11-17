//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"chat/internal/app"
	"chat/internal/config"
	"chat/internal/dto"
	"chat/internal/migrate"
)

func TestAppRunAuthentication(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	if _, err := migrate.Up(ctx, db, os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	// app.Run opens its own pool; keep it in this test's isolated schema.
	t.Setenv("PGOPTIONS", "-c search_path="+schema)
	settings := testPostgresConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	appCtx, stop := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		result <- app.Run(appCtx, config.Config{
			HTTPAddr: address, Postgres: settings, SessionCookieSecure: false,
		})
	}()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("app.Run: %v", err)
			}
		case <-time.After(12 * time.Second):
			t.Error("app.Run did not stop after cancellation")
		}
	})

	transport := &http.Transport{}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	baseURL := "http://" + address
	readyCtx, stopWaiting := context.WithTimeout(ctx, 10*time.Second)
	defer stopWaiting()
	for {
		req, err := http.NewRequestWithContext(readyCtx, http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"status":"ok"}` {
				t.Fatalf("unexpected health response: status=%d body=%q error=%v", response.StatusCode, body, readErr)
			}
			break
		}
		select {
		case <-readyCtx.Done():
			t.Fatalf("application did not become ready: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	request := func(method, path, body string, cookie *http.Cookie, crossOrigin bool, wantStatus int) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if crossOrigin {
			req.Header.Set("Origin", "https://other.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != wantStatus {
			t.Fatalf("%s %s: status=%d, want %d; body=%s", method, path, response.StatusCode, wantStatus, data)
		}
		return response, data
	}

	const registration = `{"username":"ivan","name":"Иван","password":"long personal password"}`
	request(http.MethodPost, "/auth/register", registration, nil, true, http.StatusForbidden)
	registered, body := request(http.MethodPost, "/auth/register", registration, nil, false, http.StatusCreated)
	var profile dto.UserResponse
	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID <= 0 || profile.Username != "ivan" || profile.Name != "Иван" {
		t.Fatalf("unexpected registered profile: %+v", profile)
	}
	cookies := registered.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "chat_session" || cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("app.Run must use the configured session cookie settings")
	}
	profilePath := "/users/" + strconv.FormatInt(profile.ID, 10)
	request(http.MethodGet, profilePath, "", nil, false, http.StatusUnauthorized)
	_, body = request(http.MethodGet, profilePath, "", cookies[0], false, http.StatusOK)
	var storedProfile dto.UserResponse
	if err := json.Unmarshal(body, &storedProfile); err != nil {
		t.Fatal(err)
	}

	if storedProfile.ID != profile.ID || storedProfile.Username != profile.Username || storedProfile.Name != profile.Name ||
		!storedProfile.CreatedAt.Equal(profile.CreatedAt.Truncate(time.Microsecond)) ||
		!storedProfile.UpdatedAt.Equal(profile.UpdatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("profile read = %+v, want registered profile %+v with microsecond timestamp precision", storedProfile, profile)
	}
	request(http.MethodPost, "/auth/logout", "", cookies[0], false, http.StatusNoContent)
	request(http.MethodGet, profilePath, "", cookies[0], false, http.StatusUnauthorized)

	const login = `{"username":"ivan","password":"long personal password"}`
	request(http.MethodPost, "/auth/login", login, nil, true, http.StatusForbidden)
	request(http.MethodPost, "/auth/login", login, nil, false, http.StatusOK)
	for range 9 {
		request(http.MethodPost, "/auth/login", `{"username":"missing","password":"wrong-password"}`, nil, false, http.StatusUnauthorized)
	}
	limited, body := request(http.MethodPost, "/auth/login", login, nil, false, http.StatusTooManyRequests)
	seconds, err := strconv.Atoi(limited.Header.Get("Retry-After"))
	if err != nil || seconds < 1 || seconds > 60 {
		t.Fatal("rate limit response must include Retry-After between 1 and 60 seconds")
	}
	if strings.TrimSpace(string(body)) != `{"error":"too many requests"}` || limited.Header.Get("Set-Cookie") != "" {
		t.Fatal("rate limit response must contain the public error and no new session cookie")
	}
}
