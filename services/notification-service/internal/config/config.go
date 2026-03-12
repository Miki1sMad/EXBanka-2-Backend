// Package config loads notification-service configuration from ENV vars.
// Required for Gmail SMTP: SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, FROM_EMAIL, FRONTEND_URL.
package config

import (
	"os"
	"strings"
)

// Config holds all runtime configuration for the notification service.
type Config struct {
	RabbitMQURL string // RABBITMQ_URL
	SMTPHost    string // SMTP_HOST (e.g. smtp.gmail.com)
	SMTPPort    string // SMTP_PORT (e.g. 587 for Gmail with STARTTLS)
	SMTPUser    string // SMTP_USER (e.g. your-email@gmail.com or app account)
	SMTPPass    string // SMTP_PASS (Gmail app password; never logged)
	FromEmail   string // FROM_EMAIL (sender address)
	FrontendURL string // FRONTEND_URL (base URL for activation/reset links)
}

// LoadConfig returns a Config populated from environment variables.
// Values are trimmed of leading/trailing spaces. Set env (e.g. from .env) before calling.
func LoadConfig() *Config {
	return &Config{
		RabbitMQURL: trim(getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")),
		SMTPHost:    trim(getEnv("SMTP_HOST", "smtp.gmail.com")),
		SMTPPort:    trim(getEnv("SMTP_PORT", "587")),
		SMTPUser:    trim(getEnv("SMTP_USER", "")),
		SMTPPass:    trim(getEnv("SMTP_PASS", "")),
		FromEmail:   trim(getEnv("FROM_EMAIL", "")),
		FrontendURL: trim(getEnv("FRONTEND_URL", "http://localhost:3001")),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func trim(s string) string {
	return strings.TrimSpace(s)
}
