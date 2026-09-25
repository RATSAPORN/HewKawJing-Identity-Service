package repositories

import (
	"context"
	"errors"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"identityService/models"
)

var ErrEmailExists = errors.New("email already registered")

type IdentityRepository interface {
	CreateUser(context.Context, models.User) (*models.User, error)
	GetUserByEmail(context.Context, string) (*models.User, error)
	CreateSession(context.Context, models.Session) error
	GetUserByAccessToken(context.Context, string) (*models.User, error)
	RotateSession(context.Context, string, models.Session) (*models.User, error)
	RevokeSession(context.Context, string) error
}
type identityRepository struct{ db *sqlx.DB }

func NewIdentityRepository(db *sqlx.DB) IdentityRepository { return &identityRepository{db: db} }

func (r *identityRepository) CreateUser(ctx context.Context, user models.User) (*models.User, error) {
	var created models.User
	err := r.db.GetContext(ctx, &created, `
		INSERT INTO users (display_name, email, password_hash) VALUES ($1, $2, $3)
		RETURNING user_id, display_name, email, password_hash, account_status, created_at`,
		user.DisplayName, user.Email, user.PasswordHash)
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		(pgErr.Constraint == "users_email_key" || pgErr.Constraint == "users_email_normalized_key") {
		return nil, ErrEmailExists
	}
	if err != nil {
		return nil, err
	}
	return &created, nil
}
func (r *identityRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := r.db.GetContext(ctx, &user, `
		SELECT user_id, display_name, email, password_hash, account_status, created_at
		FROM users WHERE lower(btrim(email)) = $1 AND deleted_at IS NULL`, email)
	if err != nil {
		return nil, err
	}
	return &user, nil
}
func (r *identityRepository) CreateSession(ctx context.Context, session models.Session) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO auth_sessions (user_id, access_token_hash, refresh_token_hash, access_expires_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, session.UserID, session.AccessTokenHash,
		session.RefreshTokenHash, session.AccessExpiresAt, session.ExpiresAt)
	return err
}
func (r *identityRepository) GetUserByAccessToken(ctx context.Context, hash string) (*models.User, error) {
	var user models.User
	err := r.db.GetContext(ctx, &user, `
		SELECT u.user_id, u.display_name, u.email, u.password_hash, u.account_status, u.created_at
		FROM users u JOIN auth_sessions s ON s.user_id = u.user_id
		WHERE s.access_token_hash = $1 AND s.revoked_at IS NULL
		AND s.access_expires_at > now() AND s.expires_at > now()
		AND u.account_status = 'active' AND u.deleted_at IS NULL`, hash)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// A conditional UPDATE makes refresh tokens single use, including concurrent requests.
func (r *identityRepository) RotateSession(ctx context.Context, oldHash string, session models.Session) (*models.User, error) {
	var user models.User
	err := r.db.GetContext(ctx, &user, `
		UPDATE auth_sessions s SET access_token_hash = $2, refresh_token_hash = $3,
		access_expires_at = $4, expires_at = $5 FROM users u
		WHERE s.refresh_token_hash = $1 AND s.user_id = u.user_id
		AND s.revoked_at IS NULL AND s.expires_at > now()
		AND u.account_status = 'active' AND u.deleted_at IS NULL
		RETURNING u.user_id, u.display_name, u.email, u.password_hash, u.account_status, u.created_at`,
		oldHash, session.AccessTokenHash, session.RefreshTokenHash, session.AccessExpiresAt, session.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}
func (r *identityRepository) RevokeSession(ctx context.Context, hash string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE auth_sessions SET revoked_at = now()
		WHERE access_token_hash = $1 AND revoked_at IS NULL`, hash)
	return err
}
