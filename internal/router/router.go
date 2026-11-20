package router

import (
	"net/http"

	"chat/internal/handlers"
)

func New(userHandler *handlers.UserHandler, conversationHandler *handlers.ConversationHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handlers.Health)
	mux.HandleFunc("POST /auth/register", userHandler.Register)
	mux.HandleFunc("POST /auth/login", userHandler.Login)
	mux.HandleFunc("POST /auth/logout", userHandler.Logout)
	mux.HandleFunc("GET /users/{id}", userHandler.GetByID)
	mux.HandleFunc("PUT /users/{id}/photo", userHandler.UploadPhoto)
	mux.HandleFunc("GET /users/{id}/photo", userHandler.GetPhoto)
	mux.HandleFunc("DELETE /users/{id}/photo", userHandler.DeletePhoto)
	mux.HandleFunc("POST /conversations/direct", conversationHandler.CreateDirect)
	return mux
}
