package grpcserver

import (
	"context"
	"identityService/dtos"
	apierrors "identityService/errors"
	identityv1 "identityService/gen/identity/v1"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const validToken = "good-token"

var testUser = dtos.UserDTO{ID: "u1", DisplayName: "Demo", Email: "demo@example.com",
	CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}

type stubService struct {
	registerErr *apierrors.CommonResponse
	loggedOut   []string
}

func (s *stubService) Register(_ context.Context, req dtos.RegisterRequest) (*dtos.UserDTO, *apierrors.CommonResponse) {
	if s.registerErr != nil {
		return nil, s.registerErr
	}
	user := testUser
	user.DisplayName, user.Email = req.DisplayName, req.Email
	return &user, nil
}
func (s *stubService) Login(_ context.Context, req dtos.LoginRequest) (*dtos.AuthResponse, *apierrors.CommonResponse) {
	if req.Password != "right" {
		return nil, &apierrors.ErrorInvalidCredentials
	}
	return &dtos.AuthResponse{AccessToken: "a", RefreshToken: "r", TokenType: "Bearer", ExpiresIn: 3600, User: testUser}, nil
}
func (s *stubService) Authenticate(_ context.Context, token string) (*dtos.UserDTO, *apierrors.CommonResponse) {
	if token != validToken {
		return nil, &apierrors.ErrorUnauthorized
	}
	return &testUser, nil
}
func (s *stubService) Refresh(_ context.Context, token string) (*dtos.AuthResponse, *apierrors.CommonResponse) {
	if token != "refresh" {
		return nil, &apierrors.ErrorUnauthorized
	}
	return &dtos.AuthResponse{AccessToken: "a2", RefreshToken: "r2", TokenType: "Bearer", ExpiresIn: 3600, User: testUser}, nil
}
func (s *stubService) Logout(_ context.Context, token string) *apierrors.CommonResponse {
	s.loggedOut = append(s.loggedOut, token)
	return nil
}

func dial(t *testing.T, service *stubService) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := NewServer(service, false)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func withBearer(token string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
}
func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("code = %v, want %v (err: %v)", got, want, err)
	}
}

func TestRegisterMapsFieldsAndErrors(t *testing.T) {
	service := &stubService{}
	client := identityv1.NewIdentityServiceClient(dial(t, service))
	resp, err := client.Register(context.Background(), &identityv1.RegisterRequest{
		DisplayName: "Demo", Email: "demo@example.com", Password: "password1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.User.Email != "demo@example.com" || !resp.User.CreatedAt.AsTime().Equal(testUser.CreatedAt) {
		t.Fatalf("unexpected user: %v", resp.User)
	}

	for _, tc := range []struct {
		resp *apierrors.CommonResponse
		code codes.Code
	}{
		{&apierrors.ErrorBadRequest, codes.InvalidArgument},
		{&apierrors.ErrorEmailExists, codes.AlreadyExists},
		{&apierrors.ErrorInternal, codes.Internal},
	} {
		service.registerErr = tc.resp
		_, err := client.Register(context.Background(), &identityv1.RegisterRequest{})
		assertCode(t, err, tc.code)
		if status.Convert(err).Message() != tc.resp.Message {
			t.Fatalf("message = %q, want %q", status.Convert(err).Message(), tc.resp.Message)
		}
	}
}

func TestLoginAndRefresh(t *testing.T) {
	client := identityv1.NewIdentityServiceClient(dial(t, &stubService{}))
	login, err := client.Login(context.Background(), &identityv1.LoginRequest{Email: "demo@example.com", Password: "right"})
	if err != nil {
		t.Fatal(err)
	}
	if login.Tokens.AccessToken != "a" || login.Tokens.TokenType != "Bearer" || login.Tokens.ExpiresIn != 3600 || login.User.UserId != "u1" {
		t.Fatalf("unexpected login response: %v", login)
	}
	_, err = client.Login(context.Background(), &identityv1.LoginRequest{Email: "demo@example.com", Password: "wrong"})
	assertCode(t, err, codes.Unauthenticated)

	refreshed, err := client.RefreshToken(context.Background(), &identityv1.RefreshTokenRequest{RefreshToken: "refresh"})
	if err != nil || refreshed.Tokens.AccessToken != "a2" {
		t.Fatalf("refresh = %v, %v", refreshed, err)
	}
	_, err = client.RefreshToken(context.Background(), &identityv1.RefreshTokenRequest{RefreshToken: "stale"})
	assertCode(t, err, codes.Unauthenticated)
}

func TestGetMeReadsBearerMetadata(t *testing.T) {
	client := identityv1.NewIdentityServiceClient(dial(t, &stubService{}))
	resp, err := client.GetMe(withBearer(validToken), &identityv1.GetMeRequest{})
	if err != nil || resp.User.UserId != "u1" {
		t.Fatalf("GetMe = %v, %v", resp, err)
	}
	_, err = client.GetMe(context.Background(), &identityv1.GetMeRequest{})
	assertCode(t, err, codes.Unauthenticated)
	basic := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Basic "+validToken)
	_, err = client.GetMe(basic, &identityv1.GetMeRequest{})
	assertCode(t, err, codes.Unauthenticated)
}

func TestLogoutRequiresValidToken(t *testing.T) {
	service := &stubService{}
	client := identityv1.NewIdentityServiceClient(dial(t, service))
	_, err := client.Logout(withBearer("unknown"), &identityv1.LogoutRequest{})
	assertCode(t, err, codes.Unauthenticated)
	if len(service.loggedOut) != 0 {
		t.Fatal("rejected token must not reach Logout")
	}
	if _, err := client.Logout(withBearer(validToken), &identityv1.LogoutRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(service.loggedOut) != 1 || service.loggedOut[0] != validToken {
		t.Fatalf("loggedOut = %v", service.loggedOut)
	}
}

func TestValidateAccessToken(t *testing.T) {
	client := identityv1.NewIdentityInternalServiceClient(dial(t, &stubService{}))
	resp, err := client.ValidateAccessToken(context.Background(), &identityv1.ValidateAccessTokenRequest{AccessToken: validToken})
	if err != nil || resp.User.Email != testUser.Email {
		t.Fatalf("ValidateAccessToken = %v, %v", resp, err)
	}
	_, err = client.ValidateAccessToken(context.Background(), &identityv1.ValidateAccessTokenRequest{AccessToken: "bad"})
	assertCode(t, err, codes.Unauthenticated)
}

func TestHealthServing(t *testing.T) {
	client := healthpb.NewHealthClient(dial(t, &stubService{}))
	resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil || resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health = %v, %v", resp, err)
	}
}
