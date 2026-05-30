// notification-service entrypoint.
//
// Connects to RabbitMQ and consumes EmailEvent messages,
// dispatching HTML emails via Gmail SMTP (or other SMTP) for each event received.
// Env: SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, FROM_EMAIL, FRONTEND_URL, RABBITMQ_URL.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"banka-backend/services/notification-service/internal/config"
	"banka-backend/services/notification-service/internal/service"
	"banka-backend/services/notification-service/internal/smtp"
	"banka-backend/services/notification-service/internal/transport"
	"banka-backend/shared/metrics"
)

func main() {
	// Optional: load .env from current directory (e.g. for local dev).
	if err := godotenv.Load(); err == nil {
		log.Println("[main] loaded .env")
	}
	cfg := config.LoadConfig()

	smtpSender := smtp.NewRealSender(cfg)
	emailSvc := service.NewEmailService(cfg, smtpSender)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prometheus gRPC server metrike — passed as first interceptor.
	srvMetrics := metrics.NewServerMetrics()

	// gRPC server — prima sinhronizovane zahteve od bank-service (OTP emailovi).
	grpcAddr := getEnv("GRPC_ADDR", "0.0.0.0:50053")
	go transport.StartGRPCServer(ctx, grpcAddr, emailSvc, srvMetrics.UnaryServerInterceptor())

	// RabbitMQ consumer — prima asinhroni eventi (ACTIVATION, ACCOUNT_CREATED, CARD_OTP itd.)
	go transport.StartConsumer(cfg, emailSvc)

	// Standalone HTTP listener samo za /metrics (notification-service nema gateway).
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	metricsSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("[metrics] HTTP listening on %s", cfg.MetricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[metrics] ListenAndServe error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[main] shutdown signal received, exiting")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = metricsSrv.Shutdown(shutdownCtx)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
