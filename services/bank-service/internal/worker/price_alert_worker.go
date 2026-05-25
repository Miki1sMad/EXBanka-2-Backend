package worker

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"banka-backend/services/bank-service/internal/domain"

	amqp "github.com/rabbitmq/amqp091-go"
)

// PriceAlertWorker checks active price alerts after each listing price update
// and fires email notifications when thresholds are crossed.
// It implements the worker.PriceTickPublisher interface so it can be composed
// with PriceTickBus via a composite publisher.
type PriceAlertWorker struct {
	repo    domain.PriceAlertRepository
	amqpURL string
}

func NewPriceAlertWorker(repo domain.PriceAlertRepository, amqpURL string) *PriceAlertWorker {
	return &PriceAlertWorker{repo: repo, amqpURL: amqpURL}
}

// Publish is called by ListingRefresherWorker after each successful price save.
// It checks active alerts for the listing and fires notifications when conditions are met.
func (w *PriceAlertWorker) Publish(listingID int64, ask, bid float64) {
	currentPrice := (ask + bid) / 2

	alerts, err := w.repo.ListActiveForListing(listingID)
	if err != nil {
		log.Printf("[price-alert] list alerts for listing %d: %v", listingID, err)
		return
	}

	for _, alert := range alerts {
		triggered := false
		switch alert.Direction {
		case domain.PriceAlertAbove:
			triggered = currentPrice >= alert.Threshold
		case domain.PriceAlertBelow:
			triggered = currentPrice <= alert.Threshold
		}

		if !triggered {
			continue
		}

		if err := w.repo.Deactivate(alert.ID); err != nil {
			log.Printf("[price-alert] deactivate alert %d: %v", alert.ID, err)
			continue
		}

		dirLabel := "prešla prag naviše"
		if alert.Direction == domain.PriceAlertBelow {
			dirLabel = "pala ispod praga"
		}

		event := OrderEmailEvent{
			Type:  "PRICE_ALERT",
			Email: alert.Email,
			Data: map[string]string{
				"ticker":        alert.Ticker,
				"threshold":     strconv.FormatFloat(alert.Threshold, 'f', 4, 64),
				"current_price": fmt.Sprintf("%.4f", currentPrice),
				"direction_label": dirLabel,
			},
		}

		if err := w.publishEvent(event); err != nil {
			log.Printf("[price-alert] publish alert %d: %v", alert.ID, err)
		} else {
			log.Printf("[price-alert] fired alert %d for %s (%.4f %s %.4f) → %s",
				alert.ID, alert.Ticker, currentPrice, alert.Direction, alert.Threshold, alert.Email)
		}
	}
}

func (w *PriceAlertWorker) publishEvent(event OrderEmailEvent) error {
	if w.amqpURL == "" {
		log.Printf("[price-alert] noop — RabbitMQ nije konfigurisan (PRICE_ALERT za %s)", event.Email)
		return nil
	}

	conn, err := amqp.Dial(w.amqpURL)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("amqp channel: %w", err)
	}
	defer ch.Close()

	if _, err := ch.QueueDeclare(emailQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("queue declare: %w", err)
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	return ch.Publish("", emailQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}
