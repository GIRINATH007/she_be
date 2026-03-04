package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sheguard/backend/config"
	"github.com/sheguard/backend/handlers"
	appwritemw "github.com/sheguard/backend/middleware"
)

func main() {
	// Load .env in development
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Initialize AppWrite client
	cfg := config.Load()
	config.InitAppWrite(cfg)

	e := echo.New()

	// --------------- Global Middleware ---------------
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(appwritemw.SecurityHeaders)
	e.Use(appwritemw.SanitizeMiddleware)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.POST, echo.PATCH, echo.DELETE},
		AllowHeaders: []string{echo.HeaderContentType, echo.HeaderAuthorization, "X-User-ID"},
	}))
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))

	// --------------- Public Routes ---------------
	e.GET("/health", handlers.HealthCheck)

	// --------------- Protected Routes ---------------
	api := e.Group("/api", appwritemw.AuthMiddleware)

	// Profile
	api.GET("/me", handlers.GetProfile)
	api.PATCH("/me", handlers.UpdateProfile)

	// Contacts
	api.GET("/contacts", handlers.GetContacts)
	api.POST("/contacts", handlers.AddContact)
	api.PATCH("/contacts/:id", handlers.UpdateContact)
	api.DELETE("/contacts/:id", handlers.DeleteContact)
	api.GET("/contacts/search", handlers.SearchUserByPhone)

	// SOS
	api.POST("/sos/trigger", handlers.TriggerSOS)
	api.POST("/sos/resolve", handlers.ResolveSOS)

	// Walk With Me
	api.POST("/walk/invite", handlers.InviteWalk)
	api.POST("/walk/accept/:id", handlers.AcceptWalk)
	api.POST("/walk/reject/:id", handlers.RejectWalk)
	api.POST("/walk/complete/:id", handlers.CompleteWalk)
	api.POST("/walk/cancel/:id", handlers.CancelWalk)
	api.GET("/walk/active", handlers.GetActiveWalk)

	// --------------- WebSocket (location) ---------------
	e.GET("/ws/location", handlers.LocationWebSocket)

	// --------------- Start Server ---------------
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	e.Logger.Fatal(e.Start(":" + port))
}
