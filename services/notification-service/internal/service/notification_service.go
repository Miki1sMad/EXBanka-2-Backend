// Package service contains notification use-case logic.
package service

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"strings"

	"banka-backend/services/notification-service/internal/config"
	"banka-backend/services/notification-service/internal/domain"
	"banka-backend/services/notification-service/internal/smtp"
)

//go:embed templates/activation.html
var activationTmpl string

//go:embed templates/reset.html
var resetTmpl string

//go:embed templates/activation_success.html
var activationSuccessTmpl string

//go:embed templates/password_reset_success.html
var passwordResetSuccessTmpl string

//go:embed templates/account_created.html
var accountCreatedTmpl string

//go:embed templates/card_otp.html
var cardOTPTmpl string

//go:embed templates/card_status_changed.html
var cardStatusChangedTmpl string

//go:embed templates/card_created.html
var cardCreatedTmpl string

//go:embed templates/kredit_podnet.html
var kreditPodnetTmpl string

//go:embed templates/kredit_rata_upozorenje.html
var kreditRataUpozorenjeTmpl string

//go:embed templates/account_locked.html
var accountLockedTmpl string

//go:embed templates/payment_executed.html
var paymentExecutedTmpl string

//go:embed templates/transfer_executed.html
var transferExecutedTmpl string

//go:embed templates/limit_changed.html
var limitChangedTmpl string

//go:embed templates/kredit_odobren.html
var kreditOdobrenTmpl string

//go:embed templates/order_pending.html
var orderPendingTmpl string

//go:embed templates/order_approved.html
var orderApprovedTmpl string

//go:embed templates/order_declined.html
var orderDeclinedTmpl string

//go:embed templates/order_executed.html
var orderExecutedTmpl string

//go:embed templates/order_canceled.html
var orderCanceledTmpl string

//go:embed templates/price_alert.html
var priceAlertTmpl string

//go:embed templates/recurring_order_skipped.html
var recurringOrderSkippedTmpl string

//go:embed templates/otc_counter_offer.html
var otcCounterOfferTmpl string

//go:embed templates/otc_offer_accepted.html
var otcOfferAcceptedTmpl string

//go:embed templates/otc_offer_declined.html
var otcOfferDeclinedTmpl string

//go:embed templates/otc_contract_expiring.html
var otcContractExpiringTmpl string

// emailTmplEntry holds a pre-parsed template and its email subject line.
type emailTmplEntry struct {
	subject string
	tmpl    *template.Template
}

// EmailService sends transactional emails via an injected smtp.Sender.
// Recipient is always taken from the event (e.g. from frontend/request payload).
type EmailService struct {
	cfg    *config.Config
	sender smtp.Sender
	tmpls  map[string]emailTmplEntry
}

// NewEmailService returns a ready EmailService.
// All HTML templates are parsed once here — template.Must panics on malformed
// embedded HTML, which is a programming error caught immediately at startup.
func NewEmailService(cfg *config.Config, sender smtp.Sender) *EmailService {
	must := func(name, src string) *template.Template {
		return template.Must(template.New(name).Parse(src))
	}
	return &EmailService{
		cfg:    cfg,
		sender: sender,
		tmpls: map[string]emailTmplEntry{
			"ACTIVATION":             {subject: "Activate Your EXBanka2 Account", tmpl: must("activation", activationTmpl)},
			"RESET":                  {subject: "Password Reset Request", tmpl: must("reset", resetTmpl)},
			"ACTIVATION_SUCCESS":     {subject: "Welcome to EXBanka2 \u2013 Your Account is Now Active", tmpl: must("activation_success", activationSuccessTmpl)},
			"PASSWORD_RESET_SUCCESS": {subject: "Security Alert: Your EXBanka2 Password Has Been Changed", tmpl: must("password_reset_success", passwordResetSuccessTmpl)},
			"ACCOUNT_CREATED":        {subject: "Your EXBanka2 Account Has Been Created", tmpl: must("account_created", accountCreatedTmpl)},
			"CARD_OTP":               {subject: "EXBanka2 \u2014 Card Verification Code", tmpl: must("card_otp", cardOTPTmpl)},
			"CARD_STATUS_CHANGED":    {subject: "EXBanka2 \u2014 Card Status Update", tmpl: must("card_status_changed", cardStatusChangedTmpl)},
			"KREIRANA_KARTICA":       {subject: "EXBanka2 \u2014 Nova platna kartica kreirana", tmpl: must("card_created", cardCreatedTmpl)},
			"KREDIT_PODNET":          {subject: "EXBanka2 \u2014 Zahtev za kredit primljen", tmpl: must("kredit_podnet", kreditPodnetTmpl)},
			"KREDIT_RATA_UPOZORENJE": {subject: "EXBanka2 \u2014 Upozorenje o naplati rate kredita", tmpl: must("kredit_rata_upozorenje", kreditRataUpozorenjeTmpl)},
			"ACCOUNT_LOCKED":         {subject: "EXBanka2 \u2014 Nalog privremeno zaklju\u010dan", tmpl: must("account_locked", accountLockedTmpl)},
			"PAYMENT_EXECUTED":       {subject: "EXBanka2 \u2014 Payment Executed", tmpl: must("payment_executed", paymentExecutedTmpl)},
			"TRANSFER_EXECUTED":      {subject: "EXBanka2 \u2014 Transfer Executed", tmpl: must("transfer_executed", transferExecutedTmpl)},
			"LIMIT_CHANGED":          {subject: "EXBanka2 \u2014 Account Limit Updated", tmpl: must("limit_changed", limitChangedTmpl)},
			"KREDIT_ODOBREN":   {subject: "EXBanka2 \u2014 Kredit odobren", tmpl: must("kredit_odobren", kreditOdobrenTmpl)},
			"ORDER_PENDING":    {subject: "EXBanka2 \u2014 Nalog \u010deka odobrenje", tmpl: must("order_pending", orderPendingTmpl)},
			"ORDER_APPROVED":   {subject: "EXBanka2 \u2014 Nalog odobren", tmpl: must("order_approved", orderApprovedTmpl)},
			"ORDER_DECLINED":   {subject: "EXBanka2 \u2014 Nalog odbijen", tmpl: must("order_declined", orderDeclinedTmpl)},
			"ORDER_EXECUTED":   {subject: "EXBanka2 \u2014 Nalog izvr\u0161en", tmpl: must("order_executed", orderExecutedTmpl)},
			"ORDER_CANCELED":   {subject: "EXBanka2 \u2014 Nalog otkazan", tmpl: must("order_canceled", orderCanceledTmpl)},
			"PRICE_ALERT":               {subject: "EXBanka2 \u2014 Price Alert aktiviran", tmpl: must("price_alert", priceAlertTmpl)},
			"RECURRING_ORDER_SKIPPED":   {subject: "EXBanka2 \u2014 Trajni nalog presko\u010den", tmpl: must("recurring_order_skipped", recurringOrderSkippedTmpl)},
			"OTC_COUNTER_OFFER":         {subject: "EXBanka2 \u2014 OTC kontraponuda primljena", tmpl: must("otc_counter_offer", otcCounterOfferTmpl)},
			"OTC_OFFER_ACCEPTED":        {subject: "EXBanka2 \u2014 OTC ponuda prihva\u0107ena", tmpl: must("otc_offer_accepted", otcOfferAcceptedTmpl)},
			"OTC_OFFER_DECLINED":        {subject: "EXBanka2 \u2014 OTC ponuda odbijena", tmpl: must("otc_offer_declined", otcOfferDeclinedTmpl)},
			"OTC_CONTRACT_EXPIRING":     {subject: "EXBanka2 \u2014 OTC ugovor uskoro isti\u010de", tmpl: must("otc_contract_expiring", otcContractExpiringTmpl)},
		},
	}
}

// SendEmail dispatches an HTML email based on the event type.
// Returns domain.ErrUnknownEventType for unrecognized types so callers can
// distinguish client errors from infrastructure failures.
func (s *EmailService) SendEmail(event domain.EmailEvent) error {
	recipient := strings.TrimSpace(event.Email)
	if recipient == "" {
		return fmt.Errorf("recipient email is required")
	}

	entry, ok := s.tmpls[event.Type]
	if !ok {
		return domain.ErrUnknownEventType{Type: event.Type}
	}

	var body bytes.Buffer
	if err := entry.tmpl.Execute(&body, struct {
		FrontendURL string
		Token       string
		Data        map[string]string
	}{
		FrontendURL: s.cfg.FrontendURL,
		Token:       event.Token,
		Data:        event.Data,
	}); err != nil {
		return fmt.Errorf("template execute: %w", err)
	}

	if err := s.sender.Send(recipient, entry.subject, body.String()); err != nil {
		log.Printf("[notification] send email failed type=%s recipient=%s: %v", event.Type, recipient, err)
		return err
	}
	log.Printf("[notification] email sent type=%s to %s", event.Type, recipient)
	return nil
}
