package conversation

import "errors"

var (
	ErrNotFound            = errors.New("conversation not found")
	ErrInvalidCursor       = errors.New("message cursor must be a positive integer")
	ErrAlreadyExists       = errors.New("conversation already exists")
	ErrInvalidParticipants = errors.New("direct conversation requires two distinct positive user IDs")
)
