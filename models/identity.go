package models

import "time"

type User struct {
	ID            string    `db:"user_id"`
	DisplayName   string    `db:"display_name"`
	Email         string    `db:"email"`
	PasswordHash  string    `db:"password_hash" json:"-"`
	AccountStatus string    `db:"account_status"`
	CreatedAt     time.Time `db:"created_at"`
}

type Session struct {
	UserID           string
	AccessTokenHash  string
	RefreshTokenHash string
	AccessExpiresAt  time.Time
	ExpiresAt        time.Time
}
