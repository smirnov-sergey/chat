package user

import "time"

type User struct {
	ID        int64
	Username  string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
