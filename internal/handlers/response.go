package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"chat/internal/dto"
	"chat/internal/model/session"
)

const sessionCookieName = "chat_session"

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, dto.ErrorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, response any) {
	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("marshal HTTP response: %v", err)
		status = http.StatusInternalServerError
		data = []byte(`{"error":"internal server error"}`)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(data, '\n')); err != nil {
		log.Printf("write HTTP response: %v", err)
	}
}

func setSessionCookie(w http.ResponseWriter, token *session.Token, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token.Value,
		Path:     "/",
		Expires:  token.ExpiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
