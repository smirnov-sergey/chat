//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
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

	storageDirectory := t.TempDir()
	appCtx, stop := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		result <- app.Run(appCtx, config.Config{
			HTTPAddr: address, Postgres: settings, SessionCookieSecure: false,
			StorageDir: storageDirectory, MaxPhotoSize: 5 * 1024 * 1024,
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
	otherRegistered, _ := request(http.MethodPost, "/auth/register",
		`{"username":"anna","name":"Anna","password":"another long password"}`, nil, false, http.StatusCreated)
	otherCookies := otherRegistered.Cookies()
	if len(otherCookies) != 1 {
		t.Fatal("second registration must create a session")
	}
	t.Run("profile photo upload, reading and replacement", func(t *testing.T) {
		readPhoto := func(cookie *http.Cookie, wantStatus int) []byte {
			t.Helper()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+profilePath+"/photo", nil)
			if err != nil {
				t.Fatal(err)
			}
			if cookie != nil {
				req.AddCookie(cookie)
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
				t.Fatalf("photo read: status=%d, want %d; body=%s", response.StatusCode, wantStatus, data)
			}
			if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Set-Cookie") != "" {
				t.Fatal("photo read must disable caching without replacing the session")
			}
			if wantStatus == http.StatusOK && (response.Header.Get("Content-Type") != "image/png" ||
				response.Header.Get("X-Content-Type-Options") != "nosniff" || response.ContentLength != int64(len(data))) {
				t.Fatal("photo read must return PNG with its exact content length and nosniff")
			}
			return data
		}
		readPhoto(nil, http.StatusUnauthorized)
		readPhoto(cookies[0], http.StatusNotFound)
		upload := func(data []byte, contentType string, wantStatus int) {
			t.Helper()
			req, err := http.NewRequestWithContext(ctx, http.MethodPut, baseURL+profilePath+"/photo", bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", contentType)
			req.AddCookie(cookies[0])
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != wantStatus {
				t.Fatalf("photo upload: status=%d, want %d; body=%s", response.StatusCode, wantStatus, body)
			}
			if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Set-Cookie") != "" {
				t.Fatal("photo upload must disable caching and must not replace the session")
			}
			if wantStatus == http.StatusNoContent && len(body) != 0 {
				t.Fatal("successful photo upload must have no response body")
			}
		}
		storedPhoto := func(want color.NRGBA) string {
			t.Helper()
			var filename string
			if err := db.QueryRowContext(ctx, "SELECT photo FROM users WHERE id = $1", profile.ID).Scan(&filename); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(storageDirectory, filename))
			if err != nil {
				t.Fatal(err)
			}
			for _, cookie := range []*http.Cookie{cookies[0], otherCookies[0]} {
				if received := readPhoto(cookie, http.StatusOK); !bytes.Equal(received, data) {
					t.Fatal("owner and another authenticated user must receive the current stored photo unchanged")
				}
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
				t.Fatalf("stored photo must be a 256 by 256 PNG: %v", err)
			}
			pixel := color.NRGBAModel.Convert(img.At(128, 128)).(color.NRGBA)
			// JPEG decoding may slightly change color values.
			if math.Abs(float64(pixel.R)-float64(want.R)) > 5 || math.Abs(float64(pixel.G)-float64(want.G)) > 5 ||
				math.Abs(float64(pixel.B)-float64(want.B)) > 5 || pixel.A != want.A {
				t.Fatalf("stored pixel=%+v, want approximately %+v", pixel, want)
			}
			return filename
		}
		source := image.NewNRGBA(image.Rect(0, 0, 16, 8))
		red := color.NRGBA{R: 255, A: 255}
		for y := range 8 {
			for x := range 16 {
				source.SetNRGBA(x, y, red)
			}
		}
		var jpegData bytes.Buffer
		if err := jpeg.Encode(&jpegData, source, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}
		upload(jpegData.Bytes(), "image/jpeg", http.StatusNoContent)
		first := storedPhoto(red)
		green := color.NRGBA{G: 255, A: 128}
		for y := range 8 {
			for x := range 16 {
				source.SetNRGBA(x, y, green)
			}
		}
		var pngData bytes.Buffer
		if err := png.Encode(&pngData, source); err != nil {
			t.Fatal(err)
		}
		upload(pngData.Bytes(), "image/png", http.StatusNoContent)
		second := storedPhoto(green)
		if second == first {
			t.Fatal("replacement must store a new file")
		}
		if _, err := os.Stat(filepath.Join(storageDirectory, first)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old photo must be removed after replacement: %v", err)
		}
		upload([]byte("not an image"), "image/png", http.StatusBadRequest)
		if filename := storedPhoto(green); filename != second {
			t.Fatal("invalid upload must preserve the previous photo reference and file")
		}
		entries, err := os.ReadDir(storageDirectory)
		if err != nil || len(entries) != 1 {
			t.Fatalf("storage must contain only the current photo: entries=%d, error=%v", len(entries), err)
		}
	})
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
