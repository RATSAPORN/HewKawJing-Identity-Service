package main

import (
	"context"
	"identityService/configs"
	"identityService/controller"
	"identityService/repositories"
	"identityService/routes"
	"identityService/services"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {

	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		log.Fatal("Cannot load .env; check its KEY=value syntax")
	}
	appName := "identity-service"
	db := configs.SetupDatabase(&appName)
	if db == nil {
		log.Fatal("Database connection failed; check DB_* settings in .env and start PostgreSQL")
	}
	defer db.Close()

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "dev" || appEnv == "local" || appEnv == "sit" {
		mode := "up"
		steps := 1
		if len(os.Args) >= 2 {
			mode = os.Args[1]
		}
		if len(os.Args) >= 3 {
			n, err := strconv.Atoi(os.Args[2])
			if err != nil {
				log.Fatalf("Invalid step number: %v", err)
			}
			steps = n
		}
		configs.RunMigrations(db, mode, steps)
	} else {
		log.Println("Skipping migrations: APP_ENV is not 'dev', 'local' or 'sit'")
	}

	identityRepo := repositories.NewIdentityRepository(db)

	identityService := services.NewIdentityService(identityRepo)

	identityController := controller.NewIdentityController(identityService)

	r := gin.Default()
	r.GET("/health", func(ctx *gin.Context) {
		checkCtx, cancel := context.WithTimeout(ctx.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(checkCtx); err != nil {
			ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatal(err)
	}
	api := r.Group("/api/v1")

	routes.RegisterIdentityRoutes(api, identityController)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}

}
