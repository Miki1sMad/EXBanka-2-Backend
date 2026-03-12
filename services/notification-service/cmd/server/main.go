// notification-service entrypoint.
//
// Connects to RabbitMQ and consumes EmailEvent messages,
// dispatching HTML emails via Gmail SMTP (or other SMTP) for each event received.
// Env: SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, FROM_EMAIL, FRONTEND_URL, RABBITMQ_URL.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"banka-backend/services/notification-service/internal/config"
	"banka-backend/services/notification-service/internal/service"
	"banka-backend/services/notification-service/internal/transport"
)

func main() {
	// Optional: load .env from current directory (e.g. for local dev).
	if err := godotenv.Load(); err == nil {
		log.Println("[main] loaded .env")
	}
	cfg := config.LoadConfig()

	emailSvc := service.NewEmailService(cfg)

	go transport.StartConsumer(cfg, emailSvc)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[main] shutdown signal received, exiting")
}
