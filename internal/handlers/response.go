package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"chat/internal/dto"
)

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
