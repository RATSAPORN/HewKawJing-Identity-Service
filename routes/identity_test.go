package routes_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"identityService/controller"
	"identityService/dtos"
	"identityService/models"
	"identityService/repositories"
	"identityService/routes"
	"identityService/services"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type memoryRepository struct {
	mu       sync.Mutex
	users    map[string]models.User
	sessions map[string]models.Session
	failure  error
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{users: make(map[string]models.User), sessions: make(map[string]models.Session)}
}
func (r *memoryRepository) CreateUser(_ context.Context, user models.User) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure != nil {
		return nil, r.failure
	}
	if _, exists := r.users[user.Email]; exists {
		return nil, repositories.ErrEmailExists
	}
	user.ID = fmt.Sprint(len(r.users) + 1)
	user.AccountStatus = "active"
	user.CreatedAt = time.Now().UTC()
	r.users[user.Email] = user
	return &user, nil
}
func (r *memoryRepository) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure != nil {
		return nil, r.failure
	}
	user, ok := r.users[email]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return &user, nil
}
func (r *memoryRepository) CreateSession(_ context.Context, session models.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure != nil {
		return r.failure
	}
	r.sessions[session.AccessTokenHash] = session
	return nil
}
func (r *memoryRepository) activeUser(id string) (*models.User, error) {
	for _, user := range r.users {
		if user.ID == id && user.AccountStatus == "active" {
			return &user, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (r *memoryRepository) GetUserByAccessToken(_ context.Context, hash string) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure != nil {
		return nil, r.failure
	}
	session, ok := r.sessions[hash]
	if !ok || !session.AccessExpiresAt.After(time.Now()) || !session.ExpiresAt.After(time.Now()) {
		return nil, sql.ErrNoRows
	}
	return r.activeUser(session.UserID)
}
func (r *memoryRepository) RotateSession(_ context.Context, oldHash string, next models.Session) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure != nil {
		return nil, r.failure
	}
	for key, session := range r.sessions {
		if session.RefreshTokenHash == oldHash && session.ExpiresAt.After(time.Now()) {
			user, err := r.activeUser(session.UserID)
			if err != nil {
				return nil, err
			}
			next.UserID = session.UserID
			delete(r.sessions, key)
			r.sessions[next.AccessTokenHash] = next
			return user, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (r *memoryRepository) RevokeSession(_ context.Context, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure != nil {
		return r.failure
	}
	delete(r.sessions, hash)
	return nil
}
func testRouter(repo repositories.IdentityRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes.RegisterIdentityRoutes(router.Group("/api/v1"),
		controller.NewIdentityController(services.NewIdentityService(repo)))
	return router
}
func request(t *testing.T, router http.Handler, method, path, body, token string, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/identity"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != status {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, response.Code, status, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authentication response must not be cached")
	}
	if strings.Contains(response.Body.String(), "password") && status < 400 {
		t.Fatal("password field leaked")
	}
	return response
}
func authData(t *testing.T, response *httptest.ResponseRecorder) dtos.AuthResponse {
	t.Helper()
	var envelope struct {
		Data dtos.AuthResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.AccessToken == "" || envelope.Data.RefreshToken == "" ||
		envelope.Data.AccessToken == envelope.Data.RefreshToken ||
		envelope.Data.TokenType != "Bearer" || envelope.Data.ExpiresIn != 3600 {
		t.Fatal("invalid authentication response")
	}
	return envelope.Data
}

const registerBody = `{"display_name":" Test User ","email":" Test@Example.com ","password":"Test-password-42"}`
const loginBody = `{"email":"TEST@example.com","password":"Test-password-42"}`

func exerciseAuthFlow(t *testing.T, repo repositories.IdentityRepository) {
	t.Helper()
	router := testRouter(repo)
	registered := request(t, router, "POST", "/register", registerBody, "", 201)
	var envelope struct {
		Data dtos.UserDTO `json:"data"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" || envelope.Data.DisplayName != "Test User" || envelope.Data.Email != "test@example.com" {
		t.Fatal("registration did not return normalized user")
	}
	user, err := repo.GetUserByEmail(context.Background(), "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash == "Test-password-42" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("Test-password-42")) != nil {
		t.Fatal("password was not securely hashed")
	}
	request(t, router, "POST", "/register", registerBody, "", 409)
	wrong := request(t, router, "POST", "/login", `{"email":"test@example.com","password":"wrong-password"}`, "", 401)
	unknown := request(t, router, "POST", "/login", `{"email":"missing@example.com","password":"wrong-password"}`, "", 401)
	if wrong.Body.String() != unknown.Body.String() {
		t.Fatal("login reveals whether an email exists")
	}
	request(t, router, "GET", "/me", "", "", 401)
	auth := authData(t, request(t, router, "POST", "/login", loginBody, "", 200))
	if auth.User.ID != envelope.Data.ID {
		t.Fatal("logged in as wrong user")
	}
	request(t, router, "GET", "/me", "", auth.AccessToken, 200)
	request(t, router, "GET", "/me", "", auth.RefreshToken, 401)
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.AccessToken+`"}`, "", 401)
	next := authData(t, request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 200))
	if next.AccessToken == auth.AccessToken || next.RefreshToken == auth.RefreshToken {
		t.Fatal("tokens were not rotated")
	}
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 401)
	request(t, router, "GET", "/me", "", auth.AccessToken, 401)
	request(t, router, "GET", "/me", "", next.AccessToken, 200)
	request(t, router, "POST", "/logout", "", next.AccessToken, 200)
	request(t, router, "GET", "/me", "", next.AccessToken, 401)
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+next.RefreshToken+`"}`, "", 401)
}
func TestAuthFlow(t *testing.T) { exerciseAuthFlow(t, newMemoryRepository()) }

func TestInvalidRequests(t *testing.T) {
	router := testRouter(newMemoryRepository())
	for _, body := range []string{
		`{}`, `null`, `{`, registerBody + `{}`,
		`{"display_name":" ","email":"a@example.com","password":"12345678"}`,
		`{"display_name":"A","email":"invalid","password":"12345678"}`,
		`{"display_name":"A","email":"a@example.com","password":"short"}`,
		`{"display_name":"A","email":"a@example.com","password":"` + strings.Repeat("a", 73) + `"}`,
		`{"display_name":"A","email":"a@example.com","password":"12345678","role":"admin"}`,
		strings.Repeat(" ", 16385) + registerBody,
	} {
		request(t, router, "POST", "/register", body, "", 400)
	}
	for _, body := range []string{`{}`, `{"email":"invalid","password":"password"}`, `{"email":"test@example.com"}`} {
		request(t, router, "POST", "/login", body, "", 400)
	}
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"invalid"}`, "", 401)
	request(t, router, "GET", "/me", "", "invalid", 401)
}

func TestExpiredAndDisabledSessions(t *testing.T) {
	repo := newMemoryRepository()
	router := testRouter(repo)
	request(t, router, "POST", "/register", registerBody, "", 201)
	auth := authData(t, request(t, router, "POST", "/login", loginBody, "", 200))
	for key, session := range repo.sessions {
		if key == auth.AccessToken || session.RefreshTokenHash == auth.RefreshToken {
			t.Fatal("raw token stored")
		}
		if len(key) != 64 || len(session.RefreshTokenHash) != 64 {
			t.Fatal("tokens must be SHA-256 hashed")
		}
		session.AccessExpiresAt = time.Now().Add(-time.Second)
		repo.sessions[key] = session
	}
	request(t, router, "GET", "/me", "", auth.AccessToken, 401)
	auth = authData(t, request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 200))
	for key, session := range repo.sessions {
		session.ExpiresAt = time.Now().Add(-time.Second)
		repo.sessions[key] = session
	}
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 401)
	auth = authData(t, request(t, router, "POST", "/login", loginBody, "", 200))
	user := repo.users["test@example.com"]
	user.AccountStatus = "disabled"
	repo.users[user.Email] = user
	request(t, router, "POST", "/login", loginBody, "", 401)
	request(t, router, "GET", "/me", "", auth.AccessToken, 401)
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 401)
}
func TestDatabaseErrorsArePrivate(t *testing.T) {
	repo := newMemoryRepository()
	repo.failure = errors.New("private database details")
	router := testRouter(repo)
	for _, path := range []string{"/register", "/login"} {
		body := registerBody
		if path == "/login" {
			body = loginBody
		}
		response := request(t, router, "POST", path, body, "", 500)
		if strings.Contains(response.Body.String(), "private") {
			t.Fatal("database error leaked")
		}
	}
}
