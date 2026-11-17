package handlers

import (
	"context"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"chat/internal/model/session"
)

const (
	loginRequestsPerMinute        = 10
	registrationRequestsPerMinute = 5
	maxRateLimitClients           = 10000
)

type SessionAuthenticator interface {
	Authenticate(ctx context.Context, token string) (*session.Session, error)
}

type userIDContextKey struct{}

func CrossOriginProtectionMiddleware() func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(crossOriginDenied))
	return protection.Handler
}

func AuthRateLimitMiddleware() func(http.Handler) http.Handler {
	login := newRequestLimiter(loginRequestsPerMinute, maxRateLimitClients)
	registration := newRequestLimiter(registrationRequestsPerMinute, maxRateLimitClients)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var limiter *requestLimiter

			if r.Method == http.MethodPost {
				switch r.URL.Path {
				case "/auth/login":
					limiter = login
				case "/auth/register":
					limiter = registration
				}
			}

			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Cache-Control", "no-store")

			peer, err := netip.ParseAddrPort(r.RemoteAddr)
			if err != nil {
				handleError(w, err)
				return
			}

			allowed, retryAfter := limiter.allow(peer.Addr().Unmap(), time.Now())
			if !allowed {
				seconds := (retryAfter + time.Second - 1) / time.Second
				w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
				handleError(w, errTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func SessionMiddleware(sessionAuthenticator SessionAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/health", "/auth/register", "/auth/login", "/auth/logout":
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Cache-Control", "no-store")
			cookie, err := r.Cookie(sessionCookieName)

			if err != nil || cookie.Value == "" {
				handleError(w, session.ErrInvalid)
				return
			}

			value, err := sessionAuthenticator.Authenticate(r.Context(), cookie.Value)
			if err != nil {
				handleError(w, err)
				return
			}

			ctx := context.WithValue(r.Context(), userIDContextKey{}, value.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func userIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDContextKey{}).(int64)
	return id, ok
}
