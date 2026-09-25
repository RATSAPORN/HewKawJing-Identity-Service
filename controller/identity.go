package controller

import (
	"encoding/json"
	"identityService/dtos"
	apierrors "identityService/errors"
	"identityService/services"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type IdentityController struct{ service services.IdentityService }

func NewIdentityController(service services.IdentityService) *IdentityController {
	return &IdentityController{service: service}
}

func (c *IdentityController) RegisterUser(ctx *gin.Context) {
	var req dtos.RegisterRequest
	if !bindJSON(ctx, &req) {
		return
	}
	user, resp := c.service.Register(ctx.Request.Context(), req)
	if resp != nil {
		apierrors.CommonErrorResponse(ctx, resp)
		return
	}
	ctx.JSON(http.StatusCreated, apierrors.CommonResponse{Message: "registered", Data: user})
}
func (c *IdentityController) LoginUser(ctx *gin.Context) {
	var req dtos.LoginRequest
	if !bindJSON(ctx, &req) {
		return
	}
	result, resp := c.service.Login(ctx.Request.Context(), req)
	if resp != nil {
		apierrors.CommonErrorResponse(ctx, resp)
		return
	}
	apierrors.CommonSuccessResponse(ctx, result)
}
func (c *IdentityController) RefreshToken(ctx *gin.Context) {
	var req dtos.RefreshTokenRequest
	if !bindJSON(ctx, &req) {
		return
	}
	result, resp := c.service.Refresh(ctx.Request.Context(), req.RefreshToken)
	if resp != nil {
		apierrors.CommonErrorResponse(ctx, resp)
		return
	}
	apierrors.CommonSuccessResponse(ctx, result)
}
func (c *IdentityController) RequireAuth(ctx *gin.Context) {
	user, resp := c.service.Authenticate(ctx.Request.Context(), bearerToken(ctx))
	if resp != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			ctx.Header("WWW-Authenticate", "Bearer")
		}
		apierrors.CommonErrorResponse(ctx, resp)
		ctx.Abort()
		return
	}
	ctx.Set("user", user)
	ctx.Next()
}
func (c *IdentityController) Me(ctx *gin.Context) {
	apierrors.CommonSuccessResponse(ctx, ctx.MustGet("user"))
}
func (c *IdentityController) LogoutUser(ctx *gin.Context) {
	if resp := c.service.Logout(ctx.Request.Context(), bearerToken(ctx)); resp != nil {
		apierrors.CommonErrorResponse(ctx, resp)
		return
	}
	apierrors.CommonSuccessResponse(ctx, gin.H{"message": "logged out"})
}
func bearerToken(ctx *gin.Context) string {
	parts := strings.Fields(ctx.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
func bindJSON(ctx *gin.Context, target any) bool {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16*1024)
	decoder := json.NewDecoder(ctx.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		apierrors.CommonErrorResponse(ctx, &apierrors.ErrorBadRequest)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		apierrors.CommonErrorResponse(ctx, &apierrors.ErrorBadRequest)
		return false
	}
	return true
}
