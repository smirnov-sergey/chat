package handlers

import (
	"errors"
	"log"
	"net/http"

	"chat/internal/model/conversation"
	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/photo"
	"chat/internal/validation"
)

var (
	errInternalServer        = errors.New("internal server error")
	errCrossOriginRequest    = errors.New("cross-origin request blocked")
	errInvalidUserID         = errors.New("invalid user ID")
	errInvalidConversationID = errors.New("invalid conversation ID")
	errAccessDenied          = errors.New("access denied")
	errTooManyRequests       = errors.New("too many requests")
)

func crossOriginDenied(w http.ResponseWriter, _ *http.Request) {
	handleError(w, errCrossOriginRequest)
}

func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTooManyRequests):
		writeError(w, http.StatusTooManyRequests, errTooManyRequests)
	case errors.Is(err, user.ErrNotFound):
		writeError(w, http.StatusNotFound, user.ErrNotFound)
	case errors.Is(err, conversation.ErrNotFound):
		writeError(w, http.StatusNotFound, conversation.ErrNotFound)
	case errors.Is(err, conversation.ErrInvalidParticipants):
		writeError(w, http.StatusBadRequest, conversation.ErrInvalidParticipants)
	case errors.Is(err, user.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, user.ErrPhotoNotFound)
	case errors.Is(err, errInvalidUserID):
		writeError(w, http.StatusBadRequest, errInvalidUserID)
	case errors.Is(err, errInvalidConversationID):
		writeError(w, http.StatusBadRequest, errInvalidConversationID)
	case errors.Is(err, errAccessDenied):
		writeError(w, http.StatusForbidden, errAccessDenied)
	case errors.Is(err, errCrossOriginRequest):
		writeError(w, http.StatusForbidden, errCrossOriginRequest)
	case errors.Is(err, errInvalidRequestBody):
		writeError(w, http.StatusBadRequest, errInvalidRequestBody)
	case errors.Is(err, errRequestBodyTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, errRequestBodyTooLarge)
	case errors.Is(err, errUnsupportedMediaType):
		writeError(w, http.StatusUnsupportedMediaType, errUnsupportedMediaType)
	case errors.Is(err, errUnsupportedPhotoMediaType):
		writeError(w, http.StatusUnsupportedMediaType, errUnsupportedPhotoMediaType)
	case errors.Is(err, photo.ErrFileTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, photo.ErrFileTooLarge)
	case errors.Is(err, photo.ErrInvalidImage):
		writeError(w, http.StatusBadRequest, photo.ErrInvalidImage)
	case errors.Is(err, photo.ErrInvalidDimensions):
		writeError(w, http.StatusBadRequest, photo.ErrInvalidDimensions)
	case errors.Is(err, validation.ErrInvalidUsername):
		writeError(w, http.StatusBadRequest, validation.ErrInvalidUsername)
	case errors.Is(err, validation.ErrInvalidName):
		writeError(w, http.StatusBadRequest, validation.ErrInvalidName)
	case errors.Is(err, validation.ErrInvalidPassword):
		writeError(w, http.StatusBadRequest, validation.ErrInvalidPassword)
	case errors.Is(err, session.ErrInvalid):
		writeError(w, http.StatusUnauthorized, session.ErrInvalid)
	case errors.Is(err, user.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, user.ErrInvalidCredentials)
	case errors.Is(err, user.ErrUsernameTaken):
		writeError(w, http.StatusConflict, user.ErrUsernameTaken)
	default:
		log.Printf("internal HTTP error: %v", err)
		writeError(w, http.StatusInternalServerError, errInternalServer)
	}
}
