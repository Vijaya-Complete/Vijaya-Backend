package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"vijaya-backend/internal/auth"
	"vijaya-backend/internal/config"
	"vijaya-backend/internal/database"
	"vijaya-backend/internal/handlers"
	"vijaya-backend/internal/httpx"
	"vijaya-backend/internal/middleware"
	"vijaya-backend/internal/services"
	"vijaya-backend/internal/storage"
)

func main() {
	ctx := context.Background()

	// ── 1. Config ──────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	// ── 2. Infrastructure ──────────────────────────────────────
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer db.Close()

	rdb, err := database.ConnectRedis(cfg)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	store, err := storage.NewClient(cfg)
	if err != nil {
		log.Fatalf("minio: %v", err)
	}

	// ── 3. Migrations ──────────────────────────────────────────
	migrationsDir := "db/migrations"
	if v := os.Getenv("MIGRATIONS_DIR"); v != "" {
		migrationsDir = v
	}
	if err := database.RunMigrations(ctx, db, migrationsDir); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	// ── 4. Bootstrap admin ─────────────────────────────────────
	if err := services.BootstrapAdmin(ctx, db, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Printf("WARNING: admin bootstrap: %v", err)
	}

	// ── 5. Services ────────────────────────────────────────────
	tm := auth.NewTokenManager(
		cfg.JWTAccessSecret, cfg.JWTRefreshSecret,
		cfg.JWTAccessTTLMin, cfg.JWTRefreshTTLDays,
	)

	emailSvc   := services.NewEmailService(cfg)
	notifSvc   := services.NewNotificationService(db)
	userSvc    := services.NewUserService(db)
	authSvc    := services.NewAuthService(db, rdb, tm)
	issueSvc   := services.NewIssueService(db)
	drawSvc    := services.NewDrawingService(db)
	articleSvc := services.NewArticleService(db)
	ratingSvc  := services.NewRatingService(db, drawSvc, articleSvc)
	reviewSvc  := services.NewReviewService(db, drawSvc, articleSvc, notifSvc)
	adminSvc   := services.NewAdminService(db, drawSvc, articleSvc, notifSvc)
	orderSvc   := services.NewOrderService(db)
	searchSvc  := services.NewSearchService(db)

	// ── 6. Echo ────────────────────────────────────────────────
	e := echo.New()
	e.HideBanner = true
	e.Validator = httpx.NewValidator()

	e.Use(echomw.Recover())
	e.Use(echomw.RequestID())
	e.Use(echomw.LoggerWithConfig(echomw.LoggerConfig{
		Format: "${time_rfc3339} ${method} ${uri} ${status} ${latency_human}\n",
	}))
	e.Use(middleware.CORS([]string{cfg.FrontendURL, cfg.AdminURL}))

	// ── 7. Middleware factories ────────────────────────────────
	authMW     := middleware.RequireAuth(tm)
	optionalMW := middleware.OptionalAuth(tm)
	adminMW    := echo.MiddlewareFunc(middleware.RequireAdmin)
	reviewRL   := middleware.RateLimit(rdb, 5, time.Hour, "review")

	// ── 8. Handlers ────────────────────────────────────────────
	authH    := handlers.NewAuthHandler(authSvc, emailSvc)
	userH    := handlers.NewUserHandler(userSvc, notifSvc, store)
	issueH   := handlers.NewIssueHandler(issueSvc)
	drawH    := handlers.NewDrawingHandler(drawSvc, userSvc, notifSvc, store)
	articleH := handlers.NewArticleHandler(articleSvc, userSvc, notifSvc, store)
	ratingH  := handlers.NewRatingHandler(ratingSvc, reviewSvc)
	orderH   := handlers.NewOrderHandler(orderSvc, emailSvc, notifSvc)
	adminH   := handlers.NewAdminHandler(adminSvc)
	searchH  := handlers.NewSearchHandler(searchSvc)
	contentH := handlers.NewContentHandler(adminSvc, rdb)

	// ── 9. Routes ──────────────────────────────────────────────
	api := e.Group("/api/v1")

	authH.Register(api.Group("/auth"), authMW)
	userH.Register(api, authMW)
	issueH.Register(api, authMW, adminMW)
	drawH.Register(api, authMW, optionalMW)
	articleH.Register(api, authMW, optionalMW)
	ratingH.Register(api, authMW, optionalMW, reviewRL)
	orderH.Register(api, optionalMW, authMW, adminMW)
	adminH.Register(api, authMW, adminMW)
	searchH.Register(api)
	contentH.Register(api)

	// ── 10. Health ─────────────────────────────────────────────
	e.GET("/health", func(c echo.Context) error {
		if err := db.Ping(c.Request().Context()); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy", "error": err.Error(),
			})
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "env": cfg.Env})
	})

	// ── 11. API route map (dev convenience) ────────────────────
	e.GET("/routes", func(c echo.Context) error {
		if cfg.Env != "development" {
			return echo.ErrNotFound
		}
		var routes []map[string]string
		for _, r := range e.Routes() {
			routes = append(routes, map[string]string{"method": r.Method, "path": r.Path})
		}
		return c.JSON(http.StatusOK, routes)
	})

	// ── 12. Start ──────────────────────────────────────────────
	addr := fmt.Sprintf(":%s", cfg.Port)
	go func() {
		log.Printf("🚀 Vijaya API listening on %s  [env=%s]", addr, cfg.Env)
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(shutCtx); err != nil {
		log.Fatal(err)
	}
	log.Println("Server stopped cleanly.")
}
