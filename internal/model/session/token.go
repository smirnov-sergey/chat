package session

import "time"

// Token is returned when a session is created; its Value is not stored in the database.
type Token struct {
	Value     string
	ExpiresAt time.Time
}
