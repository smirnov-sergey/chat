package conversation

import "time"

// ListItem contains the data needed to display a conversation in a user's list.
type ListItem struct {
	ID        int64
	Type      Type
	Title     string
	CreatedAt time.Time
}
