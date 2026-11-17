package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestLimiterWindow(t *testing.T) {
	limiter := newRequestLimiter(2, 4)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	first := netip.MustParseAddr("192.0.2.1")
	second := netip.MustParseAddr("2001:db8::1")
	for _, tt := range []struct {
		ip      netip.Addr
		after   time.Duration
		allowed bool
		retry   time.Duration
	}{
		{first, 0, true, 0},
		{first, time.Second, true, 0},
		{first, 2 * time.Second, false, 58 * time.Second},
		{second, 30 * time.Second, true, 0},
		{second, 31 * time.Second, true, 0},
		{second, 32 * time.Second, false, 58 * time.Second},
		{first, time.Minute, true, 0},
		{second, 90 * time.Second, true, 0},
	} {
		allowed, retry := limiter.allow(tt.ip, now.Add(tt.after))
		if allowed != tt.allowed || retry != tt.retry {
			t.Fatalf("allow(%s, +%v) = (%v, %v), want (%v, %v)", tt.ip, tt.after, allowed, retry, tt.allowed, tt.retry)
		}
	}
}

func TestRequestLimiterCapacityAndCleanup(t *testing.T) {
	limiter := newRequestLimiter(2, 2)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	first := netip.MustParseAddr("192.0.2.1")
	second := netip.MustParseAddr("192.0.2.2")
	third := netip.MustParseAddr("192.0.2.3")
	limiter.allow(first, now)
	limiter.allow(second, now.Add(10*time.Second))
	if allowed, retry := limiter.allow(third, now.Add(20*time.Second)); allowed || retry != 40*time.Second {
		t.Fatalf("full limiter = (%v, %v), want rejection until cleanup", allowed, retry)
	}
	if allowed, _ := limiter.allow(first, now.Add(30*time.Second)); !allowed {
		t.Fatal("a full table must still serve a known client within their quota")
	}
	if allowed, _ := limiter.allow(third, now.Add(time.Minute)); !allowed {
		t.Fatal("expired clients must be removed so a new client can be admitted")
	}
	if len(limiter.clients) != 2 {
		t.Fatalf("stored clients = %d, want 2", len(limiter.clients))
	}
	if _, exists := limiter.clients[first]; exists {
		t.Fatal("expired inactive client must be removed")
	}
}

func TestRequestLimiterConcurrentRequests(t *testing.T) {
	const limit = 10
	limiter := newRequestLimiter(limit, 100)
	ip := netip.MustParseAddr("192.0.2.1")
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if allowed, _ := limiter.allow(ip, now); allowed {
				accepted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted.Load() != limit {
		t.Fatalf("accepted concurrent requests = %d, want %d", accepted.Load(), limit)
	}
}

func TestAuthRateLimitMiddleware(t *testing.T) {
	for _, tt := range []struct {
		path  string
		limit int
	}{
		{"/auth/login", 10},
		{"/auth/register", 5},
	} {
		t.Run(tt.path, func(t *testing.T) {
			calls := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			})
			h := AuthRateLimitMiddleware()(next)
			for i := 0; i <= tt.limit; i++ {
				req := httptest.NewRequest(http.MethodPost, tt.path, nil)
				req.RemoteAddr = fmt.Sprintf("192.0.2.10:%d", 1000+i)
				if i == tt.limit {
					req.RemoteAddr = "[::ffff:192.0.2.10]:9999"
				}
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
				req.Header.Set("X-Real-IP", fmt.Sprintf("203.0.113.%d", i+1))
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if i < tt.limit {
					if rec.Code != http.StatusNoContent || rec.Header().Get("Retry-After") != "" {
						t.Fatalf("request %d: status = %d, want 204 without Retry-After", i+1, rec.Code)
					}
					continue
				}
				if rec.Code != http.StatusTooManyRequests || calls != tt.limit {
					t.Fatalf("status = %d, handler calls = %d; want 429, %d", rec.Code, calls, tt.limit)
				}
				retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
				if err != nil || retry < 1 || retry > 60 {
					t.Fatal("Retry-After must contain the positive seconds remaining in the window")
				}
				var body struct{ Error string }
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error != "too many requests" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
					t.Fatal("rejection must return the public error, disable caching and issue no cookies")
				}
			}
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			req.RemoteAddr = "192.0.2.11:1000"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent || calls != tt.limit+1 {
				t.Fatal("another IP must have a separate quota")
			}
		})
	}
}

func TestAuthRateLimitMiddlewareSeparateRoutes(t *testing.T) {
	h := AuthRateLimitMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tt := range []struct {
		path  string
		count int
	}{
		{"/auth/register", 5},
		{"/auth/login", 10},
	} {
		for range tt.count {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tt.path, nil))
			if rec.Code != http.StatusNoContent {
				t.Fatalf("%s status = %d, want independent route quotas", tt.path, rec.Code)
			}
		}
	}
}

func TestAuthRateLimitMiddlewareSkipsOtherRequests(t *testing.T) {
	h := AuthRateLimitMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/auth/login"},
		{http.MethodPost, "/auth/logout"},
		{http.MethodGet, "/health"},
		{http.MethodGet, "/users/42"},
		{http.MethodPost, "/auth/login/extra"},
	} {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		req.RemoteAddr = "unparsed-peer"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s status = %d, want 204", tt.method, tt.path, rec.Code)
		}
	}
}

func TestAuthRateLimitMiddlewareInvalidPeer(t *testing.T) {
	h := AuthRateLimitMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("request without a valid peer address must be rejected")
	}))
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "unparsed-peer"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != "{\"error\":\"internal server error\"}\n" {
		t.Fatalf("invalid peer status = %d, body = %q; want a generic 500", rec.Code, rec.Body.String())
	}
}
