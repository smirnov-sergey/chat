package router

import (
	"net/http"

	"chat/internal/handlers"
)

func New() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handlers.Health)

	return mux
}
