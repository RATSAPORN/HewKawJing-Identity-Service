package grpcserver

import (
	"context"
	"identityService/dtos"
	apierrors "identityService/errors"
	identityv1 "identityService/gen/identity/v1"
	"identityService/services"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Matches the 16 KiB body limit of the HTTP API.
const maxRequestBytes = 16 * 1024

// NewServer registers the identity services and the standard gRPC health service.
// Reflection lets tools like grpcurl discover the API; enable it only outside production.
func NewServer(service services.IdentityService, enableReflection bool) *grpc.Server {
	server := grpc.NewServer(grpc.MaxRecvMsgSize(maxRequestBytes))
	identityv1.RegisterIdentityServiceServer(server, &identityServer{service: service})
	identityv1.RegisterIdentityInternalServiceServer(server, &internalServer{service: service})
	healthpb.RegisterHealthServer(server, health.NewServer())
	if enableReflection {
		reflection.Register(server)
	}
	return server
}

type identityServer struct {
	identityv1.UnimplementedIdentityServiceServer
	service services.IdentityService
}

func (s *identityServer) Register(ctx context.Context, req *identityv1.RegisterRequest) (*identityv1.RegisterResponse, error) {
	user, resp := s.service.Register(ctx, dtos.RegisterRequest{
		DisplayName: req.GetDisplayName(), Email: req.GetEmail(), Password: req.GetPassword(),
	})
	if resp != nil {
		return nil, toStatus(resp)
	}
	return &identityv1.RegisterResponse{User: toUser(user)}, nil
}
func (s *identityServer) Login(ctx context.Context, req *identityv1.LoginRequest) (*identityv1.LoginResponse, error) {
	result, resp := s.service.Login(ctx, dtos.LoginRequest{Email: req.GetEmail(), Password: req.GetPassword()})
	if resp != nil {
		return nil, toStatus(resp)
	}
	return &identityv1.LoginResponse{User: toUser(&result.User), Tokens: toTokens(result)}, nil
}
func (s *identityServer) RefreshToken(ctx context.Context, req *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error) {
	result, resp := s.service.Refresh(ctx, req.GetRefreshToken())
	if resp != nil {
		return nil, toStatus(resp)
	}
	return &identityv1.RefreshTokenResponse{User: toUser(&result.User), Tokens: toTokens(result)}, nil
}
func (s *identityServer) GetMe(ctx context.Context, _ *identityv1.GetMeRequest) (*identityv1.GetMeResponse, error) {
	user, resp := s.service.Authenticate(ctx, bearerToken(ctx))
	if resp != nil {
		return nil, toStatus(resp)
	}
	return &identityv1.GetMeResponse{User: toUser(user)}, nil
}

// Logout authenticates first, like the HTTP route's RequireAuth middleware,
// so an unknown or expired token is rejected instead of silently accepted.
func (s *identityServer) Logout(ctx context.Context, _ *identityv1.LogoutRequest) (*identityv1.LogoutResponse, error) {
	token := bearerToken(ctx)
	if _, resp := s.service.Authenticate(ctx, token); resp != nil {
		return nil, toStatus(resp)
	}
	if resp := s.service.Logout(ctx, token); resp != nil {
		return nil, toStatus(resp)
	}
	return &identityv1.LogoutResponse{}, nil
}

type internalServer struct {
	identityv1.UnimplementedIdentityInternalServiceServer
	service services.IdentityService
}

func (s *internalServer) ValidateAccessToken(ctx context.Context, req *identityv1.ValidateAccessTokenRequest) (*identityv1.ValidateAccessTokenResponse, error) {
	user, resp := s.service.Authenticate(ctx, req.GetAccessToken())
	if resp != nil {
		return nil, toStatus(resp)
	}
	return &identityv1.ValidateAccessTokenResponse{User: toUser(user)}, nil
}

// bearerToken reads `authorization: Bearer <token>` from incoming metadata.
func bearerToken(ctx context.Context) string {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 {
		return ""
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func toStatus(resp *apierrors.CommonResponse) error {
	code := codes.Internal
	switch resp.StatusCode {
	case http.StatusBadRequest:
		code = codes.InvalidArgument
	case http.StatusUnauthorized:
		code = codes.Unauthenticated
	case http.StatusForbidden:
		code = codes.PermissionDenied
	case http.StatusNotFound:
		code = codes.NotFound
	case http.StatusConflict:
		code = codes.AlreadyExists
	}
	return status.Error(code, resp.Message)
}
func toUser(user *dtos.UserDTO) *identityv1.User {
	return &identityv1.User{
		UserId: user.ID, DisplayName: user.DisplayName, Email: user.Email,
		CreatedAt: timestamppb.New(user.CreatedAt),
	}
}
func toTokens(auth *dtos.AuthResponse) *identityv1.TokenPair {
	return &identityv1.TokenPair{
		AccessToken: auth.AccessToken, RefreshToken: auth.RefreshToken,
		TokenType: auth.TokenType, ExpiresIn: auth.ExpiresIn,
	}
}
