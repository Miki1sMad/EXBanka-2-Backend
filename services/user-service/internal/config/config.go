// Package config loads all configuration from environment variables.
// Clean Architecture: infrastructure layer — no business logic here.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration for user-service.
type Config struct {
	// HTTP
	HTTPAddr string // e.g. "0.0.0.0:8080"

	// gRPC
	GRPCAddr string // e.g. "0.0.0.0:50051"

	// PostgreSQL
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSchema   string // optional; sets search_path when non-empty

	// JWT
	JWTAccessSecret     string
	JWTRefreshSecret    string
	JWTActivationSecret string

	// Messaging
	RabbitMQURL string

	// Cross-service
	BankServiceAddr string // e.g. "http://bank-service:8080"
}

// Load reads ENV vars and returns a populated Config.
// Required vars: DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME.
// Optional vars fall back to sensible defaults.
func Load() (*Config, error) {
	required := []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"}
	for _, key := range required {
		if os.Getenv(key) == "" {
			return nil, fmt.Errorf("missing required env var: %s", key)
		}
	}

	return &Config{
		HTTPAddr: getEnv("HTTP_ADDR", "0.0.0.0:8080"),
		GRPCAddr: getEnv("GRPC_ADDR", "0.0.0.0:50051"),

		DBHost:     os.Getenv("DB_HOST"),
		DBPort:     os.Getenv("DB_PORT"),
		DBUser:     os.Getenv("DB_USER"),
		DBPassword: os.Getenv("DB_PASSWORD"),
		DBName:     os.Getenv("DB_NAME"),
		DBSchema:   os.Getenv("DB_SCHEMA"),

		JWTAccessSecret:     getEnv("JWT_ACCESS_SECRET", "change-me-access-secret"),
		JWTRefreshSecret:    getEnv("JWT_REFRESH_SECRET", "change-me-refresh-secret"),
		JWTActivationSecret: getEnv("JWT_ACTIVATION_SECRET", "change-me-activation-secret"),

		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),

		BankServiceAddr: getEnv("BANK_SERVICE_ADDR", ""),
	}, nil
}

// DSN returns the PostgreSQL connection string for GORM.
// When DBSchema is set, search_path is appended so unqualified table refs
// resolve to that schema (required when sharing a DB across services).
func (c *Config) DSN() string {
	base := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName,
	)
	if c.DBSchema != "" {
		base += " search_path=" + c.DBSchema
	}
	return base
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
