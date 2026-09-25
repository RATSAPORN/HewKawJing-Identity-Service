package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"identityService/dtos"
	apierrors "identityService/errors"
	"identityService/models"
	"identityService/repositories"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const accessLifetime = time.Hour
const refreshLifetime = 7 * 24 * time.Hour

// Keep unknown-email and incorrect-password checks comparable.
const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

type IdentityService interface {
	Register(context.Context, dtos.RegisterRequest) (*dtos.UserDTO, *apierrors.CommonResponse)
	Login(context.Context, dtos.LoginRequest) (*dtos.AuthResponse, *apierrors.CommonResponse)
	Authenticate(context.Context, string) (*dtos.UserDTO, *apierrors.CommonResponse)
	Refresh(context.Context, string) (*dtos.AuthResponse, *apierrors.CommonResponse)
	Logout(context.Context, string) *apierrors.CommonResponse
}
type identityService struct {
	repo repositories.IdentityRepository
}

func NewIdentityService(repo repositories.IdentityRepository) IdentityService {
	return &identityService{repo: repo}
}

func (s *identityService) Register(ctx context.Context, req dtos.RegisterRequest) (*dtos.UserDTO, *apierrors.CommonResponse) {
	name := strings.TrimSpace(req.DisplayName)
	email := normalizeEmail(req.Email)
	if name == "" || utf8.RuneCountInString(name) > 255 || !validEmail(email) ||
		utf8.RuneCountInString(req.Password) < 8 || len(req.Password) > 72 {
		resp := apierrors.ErrorBadRequest
		resp.Message = "display_name and valid email are required; password must be at least 8 characters and at most 72 bytes"
		return nil, &resp
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, &apierrors.ErrorInternal
	}
	user, err := s.repo.CreateUser(ctx, models.User{DisplayName: name, Email: email, PasswordHash: string(hash)})
	if errors.Is(err, repositories.ErrEmailExists) {
		return nil, &apierrors.ErrorEmailExists
	}
	if err != nil {
		return nil, &apierrors.ErrorInternal
	}
	result := userDTO(user)
	return &result, nil
}
func (s *identityService) Login(ctx context.Context, req dtos.LoginRequest) (*dtos.AuthResponse, *apierrors.CommonResponse) {
	email := normalizeEmail(req.Email)
	if !validEmail(email) || req.Password == "" || len(req.Password) > 72 {
		return nil, &apierrors.ErrorBadRequest
	}
	user, err := s.repo.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(req.Password))
		return nil, &apierrors.ErrorInvalidCredentials
	}
	if err != nil {
		return nil, &apierrors.ErrorInternal
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil || user.AccountStatus != "active" {
		return nil, &apierrors.ErrorInvalidCredentials
	}
	response, session, err := newSession()
	if err != nil {
		return nil, &apierrors.ErrorInternal
	}
	session.UserID = user.ID
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, &apierrors.ErrorInternal
	}
	response.User = userDTO(user)
	return response, nil
}
func (s *identityService) Authenticate(ctx context.Context, token string) (*dtos.UserDTO, *apierrors.CommonResponse) {
	if !validToken(token) {
		return nil, &apierrors.ErrorUnauthorized
	}
	user, err := s.repo.GetUserByAccessToken(ctx, tokenHash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apierrors.ErrorUnauthorized
	}
	if err != nil {
		return nil, &apierrors.ErrorInternal
	}
	result := userDTO(user)
	return &result, nil
}
func (s *identityService) Refresh(ctx context.Context, token string) (*dtos.AuthResponse, *apierrors.CommonResponse) {
	if !validToken(token) {
		return nil, &apierrors.ErrorUnauthorized
	}
	response, session, err := newSession()
	if err != nil {
		return nil, &apierrors.ErrorInternal
	}
	user, err := s.repo.RotateSession(ctx, tokenHash(token), session)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apierrors.ErrorUnauthorized
	}
	if err != nil {
		return nil, &apierrors.ErrorInternal
	}
	response.User = userDTO(user)
	return response, nil
}
func (s *identityService) Logout(ctx context.Context, token string) *apierrors.CommonResponse {
	if !validToken(token) {
		return &apierrors.ErrorUnauthorized
	}
	if err := s.repo.RevokeSession(ctx, tokenHash(token)); err != nil {
		return &apierrors.ErrorInternal
	}
	return nil
}
func newSession() (*dtos.AuthResponse, models.Session, error) {
	accessToken, err := randomToken()
	if err != nil {
		return nil, models.Session{}, err
	}
	refreshToken, err := randomToken()
	if err != nil {
		return nil, models.Session{}, err
	}
	now := time.Now().UTC()
	return &dtos.AuthResponse{AccessToken: accessToken, RefreshToken: refreshToken,
			TokenType: "Bearer", ExpiresIn: int64(accessLifetime.Seconds())}, models.Session{
			AccessTokenHash: tokenHash(accessToken), RefreshTokenHash: tokenHash(refreshToken),
			AccessExpiresAt: now.Add(accessLifetime), ExpiresAt: now.Add(refreshLifetime),
		}, nil
}
func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}
func validToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == token
}
func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
func validEmail(email string) bool {
	if len(email) > 255 {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && strings.Contains(email, "@")
}
func userDTO(user *models.User) dtos.UserDTO {
	return dtos.UserDTO{ID: user.ID, DisplayName: user.DisplayName, Email: user.Email, CreatedAt: user.CreatedAt}
}
