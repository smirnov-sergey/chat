package handlers

import (
	"net/http"

	"chat/internal/dto"
)

func Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, dto.HealthResponse{Status: "ok"})
}
