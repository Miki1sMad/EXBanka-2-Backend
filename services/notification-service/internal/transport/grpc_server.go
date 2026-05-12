// Package transport manages inbound connections for notification-service.
package transport

import (
	"context"
	"errors"
	"log"
	"net"

	notifv1 "banka-backend/proto/notification"
	"banka-backend/services/notification-service/internal/domain"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NotificationGRPCServer implementira notifv1.NotificationServiceServer.
//
// SendEmail RPC prima tip događaja (Subject) i token (Body) od bank-service-a
// i delegira EmailService-u koji renderuje template iz embedded fajlova —
// isti mehanizam kao RabbitMQ consumer putanja.
type NotificationGRPCServer struct {
	notifv1.UnimplementedNotificationServiceServer
	emailSvc domain.NotificationService
}

// NewNotificationGRPCServer kreira novi gRPC server koji koristi dati EmailService.
func NewNotificationGRPCServer(emailSvc domain.NotificationService) *NotificationGRPCServer {
	return &NotificationGRPCServer{emailSvc: emailSvc}
}

// HealthCheck proverava da li je servis u životu.
func (s *NotificationGRPCServer) HealthCheck(_ context.Context, _ *notifv1.HealthCheckRequest) (*notifv1.HealthCheckResponse, error) {
	return &notifv1.HealthCheckResponse{Status: "SERVING"}, nil
}

// SendEmail prima tip događaja u Subject polju i token u Body polju.
// Delegira EmailService-u koji renderuje odgovarajući HTML template.
// Konvencija: Subject = tip događaja (npr. "CARD_OTP"), Body = token/OTP kod.
func (s *NotificationGRPCServer) SendEmail(_ context.Context, req *notifv1.SendEmailRequest) (*notifv1.SendEmailResponse, error) {
	if req.GetTo() == "" {
		return nil, status.Error(codes.InvalidArgument, "polje 'to' je obavezno")
	}
	if req.GetSubject() == "" {
		return nil, status.Error(codes.InvalidArgument, "polje 'subject' je obavezno")
	}

	event := domain.EmailEvent{
		Type:  req.GetSubject(),
		Email: req.GetTo(),
		Token: req.GetBody(),
	}
	if err := s.emailSvc.SendEmail(event); err != nil {
		var errUnknown domain.ErrUnknownEventType
		if errors.As(err, &errUnknown) {
			return nil, status.Errorf(codes.InvalidArgument, "nepoznat tip emaila: %s", errUnknown.Type)
		}
		log.Printf("[grpc] SendEmail failed to=%s type=%q: %v", req.GetTo(), req.GetSubject(), err)
		return nil, status.Errorf(codes.Internal, "slanje emaila nije uspelo: %v", err)
	}

	log.Printf("[grpc] SendEmail uspešno poslat na %s (type=%s)", req.GetTo(), req.GetSubject())
	return &notifv1.SendEmailResponse{Success: true}, nil
}

// StartGRPCServer pokreće gRPC server na datoj adresi i blokira dok se ctx ne otkaže.
// Dizajniran da se poziva kao goroutine iz main-a.
// Graceful shutdown se pokreće automatski kada ctx bude otkazan.
//
// extraInterceptors se ulančavaju kao unary server interceptori (npr. Prometheus
// metrike). Stream RPC-ovi se trenutno ne koriste u notification-service.
func StartGRPCServer(ctx context.Context, addr string, emailSvc domain.NotificationService, extraInterceptors ...grpc.UnaryServerInterceptor) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("[grpc] listen %s: %v", addr, err)
	}

	var opts []grpc.ServerOption
	if len(extraInterceptors) > 0 {
		opts = append(opts, grpc.ChainUnaryInterceptor(extraInterceptors...))
	}
	srv := grpc.NewServer(opts...)
	notifv1.RegisterNotificationServiceServer(srv, NewNotificationGRPCServer(emailSvc))

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	log.Printf("[grpc] notification-service gRPC listening on %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Printf("[grpc] serve stopped: %v", err)
	}
}
