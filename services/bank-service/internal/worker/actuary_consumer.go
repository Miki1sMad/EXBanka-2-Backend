// Package worker — RabbitMQ consumer that auto-provisions actuary profiles.
package worker

import (
	"context"
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/shopspring/decimal"

	"banka-backend/services/bank-service/internal/domain"
)

const userCreatedQueue = "user_created"

// UserCreatedEvent mirrors the payload published by user-service when a new
// employee is created. Must stay in sync with utils.UserCreatedEvent.
type UserCreatedEvent struct {
	EmployeeID  int64    `json:"employee_id"`
	Email       string   `json:"email"`
	Permissions []string `json:"permissions"`
}

// StartActuaryConsumer dials RabbitMQ, declares the user_created queue, and
// begins consuming UserCreatedEvent messages. For each event it inspects the
// permissions slice: if SUPERVISOR or AGENT is present it creates the
// corresponding actuary_info record with default values.
//
// Designed to be called as a goroutine from main. Blocks until the broker
// closes the connection or ctx is cancelled.
func StartActuaryConsumer(ctx context.Context, amqpURL string, repo domain.ActuaryRepository) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		log.Fatalf("[actuary-consumer] failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("[actuary-consumer] failed to open channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		userCreatedQueue,
		true,  // durable — survives broker restart
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		log.Fatalf("[actuary-consumer] failed to declare queue: %v", err)
	}

	msgs, err := ch.Consume(
		userCreatedQueue,
		"bank-service-actuary", // consumer tag
		false,                  // manual ack
		false,                  // exclusive
		false,                  // no-local
		false,                  // no-wait
		nil,
	)
	if err != nil {
		log.Fatalf("[actuary-consumer] failed to register consumer: %v", err)
	}

	log.Printf("[actuary-consumer] started, waiting for messages on queue %q", userCreatedQueue)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				handleUserCreated(ctx, msg, repo)
			}
		}
	}()

	// Block until broker closes the connection or context is cancelled.
	select {
	case <-ctx.Done():
	case connErr := <-conn.NotifyClose(make(chan *amqp.Error, 1)):
		if connErr != nil {
			log.Printf("[actuary-consumer] connection closed: %v", connErr)
		}
	}
}

// handleUserCreated processes a single UserCreatedEvent delivery.
func handleUserCreated(ctx context.Context, msg amqp.Delivery, repo domain.ActuaryRepository) {
	var event UserCreatedEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("[actuary-consumer] malformed message — discarding: %v", err)
		msg.Ack(false)
		return
	}

	actuaryType, ok := resolveActuaryType(event.Permissions)
	if !ok {
		// Employee is neither SUPERVISOR nor AGENT — no actuary record needed.
		msg.Ack(false)
		return
	}

	needApproval := actuaryType == domain.ActuaryTypeAgent

	input := domain.CreateActuaryInput{
		EmployeeID:   event.EmployeeID,
		ActuaryType:  actuaryType,
		Limit:        decimal.Zero,
		UsedLimit:    decimal.Zero,
		NeedApproval: needApproval,
	}

	if _, err := repo.Create(ctx, input); err != nil {
		log.Printf("[actuary-consumer] failed to create actuary for employee %d: %v — requeueing", event.EmployeeID, err)
		msg.Nack(false, true)
		return
	}

	log.Printf("[actuary-consumer] actuary profile created: employee_id=%d type=%s", event.EmployeeID, actuaryType)
	msg.Ack(false)
}

// resolveActuaryType returns the ActuaryType implied by the permission codes.
// SUPERVISOR takes precedence over AGENT when both are present.
func resolveActuaryType(permissions []string) (domain.ActuaryType, bool) {
	hasSupervisor := false
	hasAgent := false
	for _, p := range permissions {
		switch p {
		case "SUPERVISOR":
			hasSupervisor = true
		case "AGENT":
			hasAgent = true
		}
	}
	if hasSupervisor {
		return domain.ActuaryTypeSupervisor, true
	}
	if hasAgent {
		return domain.ActuaryTypeAgent, true
	}
	return "", false
}
