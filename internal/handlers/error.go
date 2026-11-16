package handlers

import (
	"errors"
	"log"
	"net/http"

	"chat/internal/model/user"
	"chat/internal/validation"
)

var (
	errInternalServer     = errors.New("internal server error")
	errCrossOriginRequest = errors.New("cross-origin request blocked")
)

func CrossOriginDenied(w http.ResponseWriter, _ *http.Request) {
	handleError(w, errCrossOriginRequest)
}

func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCrossOriginRequest):
		writeError(w, http.StatusForbidden, errCrossOriginRequest)
	case errors.Is(err, errInvalidRequestBody):
		writeError(w, http.StatusBadRequest, errInvalidRequestBody)
	case errors.Is(err, errRequestBodyTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, errRequestBodyTooLarge)
	case errors.Is(err, errUnsupportedMediaType):
		writeError(w, http.StatusUnsupportedMediaType, errUnsupportedMediaType)
	case errors.Is(err, validation.ErrInvalidUsername):
		writeError(w, http.StatusBadRequest, validation.ErrInvalidUsername)
	case errors.Is(err, validation.ErrInvalidName):
		writeError(w, http.StatusBadRequest, validation.ErrInvalidName)
	case errors.Is(err, validation.ErrInvalidPassword):
		writeError(w, http.StatusBadRequest, validation.ErrInvalidPassword)
	case errors.Is(err, user.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, user.ErrInvalidCredentials)
	case errors.Is(err, user.ErrUsernameTaken):
		writeError(w, http.StatusConflict, user.ErrUsernameTaken)
	default:
		log.Printf("internal HTTP error: %v", err)
		writeError(w, http.StatusInternalServerError, errInternalServer)
	}
}
