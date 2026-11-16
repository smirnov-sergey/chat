package router

import (
	"net/http"

	"chat/internal/handlers"
)

func New(userHandler *handlers.UserHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handlers.Health)
	mux.HandleFunc("POST /auth/register", userHandler.Register)
	mux.HandleFunc("POST /auth/login", userHandler.Login)

	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(handlers.CrossOriginDenied))
	return protection.Handler(mux)
}
