package dto

import (
	"time"

	"chat/internal/model/conversation"
)

type ConversationDirectRequest struct {
	RecipientUsername string `json:"recipient_username"`
}

type ConversationGroupRequest struct {
	Name      string   `json:"name"`
	Usernames []string `json:"usernames"`
}

type ConversationGroupResponse struct {
	ConversationResponse
	OwnerUserID int64  `json:"owner_user_id"`
	Name        string `json:"name"`
}

func ToConversationGroupResponse(group *conversation.Group) ConversationGroupResponse {
	return ConversationGroupResponse{
		ConversationResponse: ToConversationResponse(&group.Conversation),
		OwnerUserID:          group.OwnerUserID,
		Name:                 group.Name,
	}
}

type ConversationResponse struct {
	ID        int64             `json:"id"`
	Type      conversation.Type `json:"type"`
	CreatedAt time.Time         `json:"created_at"`
}

type ConversationListItemResponse struct {
	ID        int64             `json:"id"`
	Type      conversation.Type `json:"type"`
	Title     string            `json:"title"`
	CreatedAt time.Time         `json:"created_at"`
}

func ToConversationListItemResponse(item conversation.ListItem) ConversationListItemResponse {
	return ConversationListItemResponse{
		ID:        item.ID,
		Type:      item.Type,
		Title:     item.Title,
		CreatedAt: item.CreatedAt.UTC(),
	}
}

func ToConversationResponse(c *conversation.Conversation) ConversationResponse {
	return ConversationResponse{
		ID:        c.ID,
		Type:      c.Type,
		CreatedAt: c.CreatedAt.UTC(),
	}
}
