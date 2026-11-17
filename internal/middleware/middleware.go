package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"

	"vijaya-backend/internal/auth"
)

const (
	CtxUserID = "user_id"
	CtxEmail  = "email"
	CtxRole   = "role"
)

// ── Auth middleware ────────────────────────────────────────────

func RequireAuth(tm *auth.TokenManager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				return echo.NewHTTPError(http.StatusUnauthorized, map[string]string{
					"error_code": "MISSING_TOKEN", "message": "Authorization header required",
				})
			}
			claims, err := tm.ValidateAccessToken(strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, map[string]string{
					"error_code": "INVALID_TOKEN", "message": "Invalid or expired access token",
				})
			}
			c.Set(CtxUserID, claims.UserID)
			c.Set(CtxEmail, claims.Email)
			c.Set(CtxRole, claims.Role)
			return next(c)
		}
	}
}

func OptionalAuth(tm *auth.TokenManager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if h := c.Request().Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				if claims, err := tm.ValidateAccessToken(strings.TrimPrefix(h, "Bearer ")); err == nil {
					c.Set(CtxUserID, claims.UserID)
					c.Set(CtxEmail, claims.Email)
					c.Set(CtxRole, claims.Role)
				}
			}
			return next(c)
		}
	}
}

func RequireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if role, _ := c.Get(CtxRole).(string); role != "admin" {
			return echo.NewHTTPError(http.StatusForbidden, map[string]string{
				"error_code": "ADMIN_REQUIRED", "message": "Admin privileges required",
			})
		}
		return next(c)
	}
}

func UserIDFromContext(c echo.Context) (uuid.UUID, bool) {
	id, ok := c.Get(CtxUserID).(uuid.UUID)
	return id, ok
}

// ── CORS ──────────────────────────────────────────────────────

func CORS(origins []string) echo.MiddlewareFunc {
	return echomw.CORSWithConfig(echomw.CORSConfig{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "Accept-Language"},
		AllowCredentials: true,
	})
}

// ── IP helpers ─────────────────────────────────────────────────

func GuestIP(c echo.Context) string {
	if fwd := c.Request().Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
	if err != nil {
		return c.Request().RemoteAddr
	}
	return host
}

// ── Rate limiting ──────────────────────────────────────────────

func RateLimit(rdb *redis.Client, limit int, window time.Duration, prefix string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key := fmt.Sprintf("ratelimit:%s:%s", prefix, GuestIP(c))
			ctx := context.Background()
			count, err := rdb.Incr(ctx, key).Result()
			if err != nil {
				return next(c) // fail open
			}
			if count == 1 {
				rdb.Expire(ctx, key, window)
			}
			if count > int64(limit) {
				return echo.NewHTTPError(http.StatusTooManyRequests, map[string]string{
					"error_code": "RATE_LIMITED", "message": "Too many requests — please slow down",
				})
			}
			return next(c)
		}
	}
}
