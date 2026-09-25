package routes

import (
	"github.com/gin-gonic/gin"
	"identityService/controller"
)

func RegisterIdentityRoutes(router *gin.RouterGroup, identity *controller.IdentityController) {
	group := router.Group("/identity")
	group.Use(func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		ctx.Next()
	})
	group.POST("/register", identity.RegisterUser)
	group.POST("/login", identity.LoginUser)
	group.POST("/refresh-token", identity.RefreshToken)
	group.GET("/me", identity.RequireAuth, identity.Me)
	group.POST("/logout", identity.RequireAuth, identity.LogoutUser)
}
