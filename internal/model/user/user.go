package user

import "time"

type User struct {
	ID           int64
	Username     string
	Name         string
	PasswordHash string `json:"-"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
