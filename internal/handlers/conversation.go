package handlers

import (
	"context"
	"net/http"
	"strconv"

	"chat/internal/dto"
	"chat/internal/model/conversation"
	"chat/internal/model/session"
)

const maxConversationDirectBodyBytes = 4 << 10

type ConversationService interface {
	GetByID(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error)
	CreateDirect(ctx context.Context, currentUserID int64, recipientUsername string) (*conversation.Conversation, error)
}

type ConversationHandler struct {
	conversationService ConversationService
}

func NewConversationHandler(conversationService ConversationService) *ConversationHandler {
	return &ConversationHandler{conversationService: conversationService}
}

func (h *ConversationHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	currentUserID, ok := userIDFromContext(r.Context())
	if !ok {
		handleError(w, session.ErrInvalid)
		return
	}

	conversationID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || conversationID <= 0 {
		handleError(w, errInvalidConversationID)
		return
	}

	c, err := h.conversationService.GetByID(r.Context(), conversationID, currentUserID)
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, dto.ToConversationResponse(c))
}

func (h *ConversationHandler) CreateDirect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	currentUserID, ok := userIDFromContext(r.Context())
	if !ok {
		handleError(w, session.ErrInvalid)
		return
	}

	var request *dto.ConversationDirectRequest
	if err := decodeJSONRequest(w, r, maxConversationDirectBodyBytes, &request); err != nil {
		handleError(w, err)
		return
	}

	if request == nil {
		handleError(w, errInvalidRequestBody)
		return
	}

	c, err := h.conversationService.CreateDirect(r.Context(), currentUserID, request.RecipientUsername)
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, dto.ToConversationResponse(c))
}
