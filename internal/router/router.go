package router

import (
	"net/http"

	"chat/internal/handlers"
)

func New(registration *handlers.UserRegistrationHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handlers.Health)
	mux.HandleFunc("POST /auth/register", registration.Register)

	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(handlers.CrossOriginDenied))
	return protection.Handler(mux)
}
