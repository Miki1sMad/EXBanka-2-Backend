// Package service contains notification use-case logic.
package service

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"strings"

	"banka-backend/services/notification-service/internal/config"
	"banka-backend/services/notification-service/internal/domain"
	"banka-backend/services/notification-service/internal/smtp"
)

// EmailService sends transactional emails via Gmail SMTP (or other SMTP).
// Recipient is always taken from the event (e.g. from frontend/request payload).
type EmailService struct {
	cfg *config.Config
}

// NewEmailService returns a ready EmailService.
func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{cfg: cfg}
}

// SendEmail dispatches an HTML email based on the event type.
// event.Email is the recipient (dynamic from frontend form / request / RabbitMQ payload).
// Links: activation = FRONTEND_URL/activate?token=...; reset = FRONTEND_URL/reset-password?token=...
func (s *EmailService) SendEmail(event domain.EmailEvent) error {
	recipient := strings.TrimSpace(event.Email)
	if recipient == "" {
		return fmt.Errorf("recipient email is required")
	}

	var subject, tmplStr string
	switch event.Type {
	case "ACTIVATION":
		subject = "Activate Your EXBanka Account"
		tmplStr = "<h1>Welcome to EXBanka!</h1><p>Click <a href='{{.FrontendURL}}/activate?token={{.Token}}'>here</a> to set your password and activate your account.</p>"
	case "RESET":
		subject = "Password Reset Request"
		tmplStr = "<h1>Password Reset</h1><p>Click <a href='{{.FrontendURL}}/reset-password?token={{.Token}}'>here</a> to reset your password.</p>"
	case "CONFIRMATION":
		subject = "Password Changed Successfully"
		tmplStr = "<h1>Password Changed</h1><p>Your password has been successfully updated. If you did not make this change, please contact support immediately.</p>"
	default:
		return fmt.Errorf("unknown email event type: %s", event.Type)
	}

	tmpl, err := template.New("email").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse: %w", err)
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, struct {
		FrontendURL string
		Token       string
	}{
		FrontendURL: s.cfg.FrontendURL,
		Token:       event.Token,
	}); err != nil {
		return fmt.Errorf("template execute: %w", err)
	}

	if err := smtp.Send(s.cfg, recipient, subject, body.String()); err != nil {
		log.Printf("[notification] send email failed type=%s recipient=%s: %v", event.Type, recipient, err)
		return err
	}
	log.Printf("[notification] email sent type=%s to %s", event.Type, recipient)
	return nil
}
