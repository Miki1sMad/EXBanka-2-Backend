// Package smtp provides a reusable email sender over SMTP with STARTTLS support
// (e.g. Gmail on port 587). It reads configuration from the notification-service
// config and never logs sensitive data (e.g. SMTP password).
package smtp

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/smtp"
	"strings"

	"banka-backend/services/notification-service/internal/config"
)

// Send sends a single HTML email to the given recipient using the configured
// SMTP server. For Gmail (smtp.gmail.com:587) it uses STARTTLS and app password auth.
// recipient must be the destination address (e.g. from the frontend/request payload).
func Send(cfg *config.Config, recipient string, subject string, bodyHTML string) error {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return fmt.Errorf("smtp: recipient email is required")
	}

	from := strings.TrimSpace(cfg.FromEmail)
	if from == "" {
		return fmt.Errorf("smtp: from email is required")
	}

	addr := fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort)
	msg := buildMessage(from, recipient, subject, bodyHTML)

	var auth smtp.Auth
	if cfg.SMTPUser != "" && cfg.SMTPPass != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	}

	// Use explicit STARTTLS for port 587 (Gmail). Standard smtp.SendMail also
	// upgrades when the server advertises STARTTLS; this path ensures we use
	// TLS for the auth and payload on port 587.
	if cfg.SMTPPort == "587" {
		err := sendWithSTARTTLS(addr, cfg.SMTPHost, auth, from, recipient, []byte(msg))
		if err != nil {
			log.Printf("[smtp] send failed to %s: %v", recipient, err)
			return err
		}
		log.Printf("[smtp] email sent successfully to %s", recipient)
		return nil
	}

	err := smtp.SendMail(addr, auth, from, []string{recipient}, []byte(msg))
	if err != nil {
		log.Printf("[smtp] send failed to %s: %v", recipient, err)
		return err
	}
	log.Printf("[smtp] email sent successfully to %s", recipient)
	return nil
}

// sendWithSTARTTLS connects to the server, upgrades to TLS via STARTTLS, then
// authenticates and sends. Used for Gmail (port 587).
func sendWithSTARTTLS(addr, hostname string, auth smtp.Auth, from, to string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()

	if err = client.Hello("localhost"); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	if ok, _ := client.Extension("STARTTLS"); !ok {
		return fmt.Errorf("smtp server %s does not support STARTTLS", addr)
	}

	tlsConfig := &tls.Config{ServerName: hostname}
	if err = client.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}

	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err = client.Mail(from); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return client.Quit()
}

func buildMessage(from, to, subject, bodyHTML string) string {
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n\r\n%s",
		from, to, subject, bodyHTML,
	)
}
