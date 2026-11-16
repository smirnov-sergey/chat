package handlers

import (
	"context"
	"net/http"

	"chat/internal/model/session"
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

func userIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDContextKey{}).(int64)
	return id, ok
}

func SessionMiddleware(sessionAuthenticator SessionAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/health", "/auth/register", "/auth/login":
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
