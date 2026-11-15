package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	Env         string
	FrontendURL string
	AdminURL    string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	RedisHost     string
	RedisPort     string
	RedisPassword string

	MinioEndpoint  string
	MinioUser      string
	MinioPassword  string
	MinioBucket    string
	MinioUseSSL    bool
	MinioPublicURL string

	JWTAccessSecret   string
	JWTRefreshSecret  string
	JWTAccessTTLMin   int
	JWTRefreshTTLDays int

	ResendAPIKey string
	EmailFrom    string

	AdminEmail    string
	AdminPassword string
}

func Load() (*Config, error) {
	_ = godotenv.Load()
	return &Config{
		Port:        env("PORT", "8080"),
		Env:         env("ENV", "development"),
		FrontendURL: env("FRONTEND_URL", "http://localhost:5173"),
		AdminURL:    env("ADMIN_URL", "http://localhost:5173/admin.html"),

		DBHost:     env("DB_HOST", "localhost"),
		DBPort:     env("DB_PORT", "5432"),
		DBUser:     env("DB_USER", "vijaya"),
		DBPassword: env("DB_PASSWORD", "vijaya_dev_password"),
		DBName:     env("DB_NAME", "vijaya_db"),
		DBSSLMode:  env("DB_SSLMODE", "disable"),

		RedisHost:     env("REDIS_HOST", "localhost"),
		RedisPort:     env("REDIS_PORT", "6379"),
		RedisPassword: env("REDIS_PASSWORD", ""),

		MinioEndpoint:  env("MINIO_ENDPOINT", "localhost:9000"),
		MinioUser:      env("MINIO_ROOT_USER", "vijaya_admin"),
		MinioPassword:  env("MINIO_ROOT_PASSWORD", "vijaya_minio_password"),
		MinioBucket:    env("MINIO_BUCKET", "vijaya-uploads"),
		MinioUseSSL:    envBool("MINIO_USE_SSL", false),
		MinioPublicURL: env("MINIO_PUBLIC_URL", "http://localhost:9000/vijaya-uploads"),

		JWTAccessSecret:   env("JWT_ACCESS_SECRET", "dev_access_secret_change_in_prod"),
		JWTRefreshSecret:  env("JWT_REFRESH_SECRET", "dev_refresh_secret_change_in_prod"),
		JWTAccessTTLMin:   envInt("JWT_ACCESS_TTL_MINUTES", 15),
		JWTRefreshTTLDays: envInt("JWT_REFRESH_TTL_DAYS", 7),

		ResendAPIKey: env("RESEND_API_KEY", ""),
		EmailFrom:    env("EMAIL_FROM", "Vijaya Magazine <noreply@vijaya.lk>"),

		AdminEmail:    env("ADMIN_EMAIL", "admin@vijaya.lk"),
		AdminPassword: env("ADMIN_PASSWORD", "Vijaya@2025"),
	}, nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode)
}
func (c *Config) RedisAddr() string { return fmt.Sprintf("%s:%s", c.RedisHost, c.RedisPort) }

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if i, e := strconv.Atoi(v); e == nil {
			return i
		}
	}
	return d
}
func envBool(k string, d bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, e := strconv.ParseBool(v); e == nil {
			return b
		}
	}
	return d
}
