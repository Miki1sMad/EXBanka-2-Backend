// Package utils — RabbitMQ publisher for async email notifications.
package utils

import (
	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
)

const emailQueue = "email_notifications"
const userCreatedQueue = "user_created"

// EmailEvent is the message payload published to the email_notifications queue.
// The Notification Service consumes this and dispatches the appropriate email.
type EmailEvent struct {
	Type  string `json:"type"`  // "ACTIVATION" | "RESET" | "CONFIRMATION"
	Email string `json:"email"` // recipient
	Token string `json:"token"` // JWT for the action link
}

// EmailPublisher abstracts RabbitMQ message publishing for testability.
// The production implementation is AMQPPublisher; tests inject a mock.
type EmailPublisher interface {
	Publish(event EmailEvent) error
}

// AMQPPublisher is the production RabbitMQ publisher.
// It dials a new connection per Publish call — suitable for low-frequency
// fire-and-forget notifications.
type AMQPPublisher struct {
	amqpURL string
}

// NewAMQPPublisher creates a real RabbitMQ publisher bound to amqpURL.
func NewAMQPPublisher(amqpURL string) *AMQPPublisher {
	return &AMQPPublisher{amqpURL: amqpURL}
}

// Publish delegates to the package-level PublishEmailEvent function.
func (p *AMQPPublisher) Publish(event EmailEvent) error {
	return PublishEmailEvent(p.amqpURL, event)
}

// UserCreatedEvent is the payload published to the user_created queue when a
// new employee is created. bank-service consumes this to auto-provision actuary profiles.
type UserCreatedEvent struct {
	EmployeeID  int64    `json:"employee_id"`
	Email       string   `json:"email"`
	Permissions []string `json:"permissions"`
}

// UserCreatedPublisher abstracts publishing UserCreatedEvent for testability.
type UserCreatedPublisher interface {
	Publish(event UserCreatedEvent) error
}

// AMQPUserCreatedPublisher is the production RabbitMQ publisher for user-created events.
type AMQPUserCreatedPublisher struct {
	amqpURL string
}

// NewAMQPUserCreatedPublisher creates a publisher bound to amqpURL.
func NewAMQPUserCreatedPublisher(amqpURL string) *AMQPUserCreatedPublisher {
	return &AMQPUserCreatedPublisher{amqpURL: amqpURL}
}

// Publish dials RabbitMQ, declares the durable user_created queue, and publishes
// the event as a persistent JSON message.
func (p *AMQPUserCreatedPublisher) Publish(event UserCreatedEvent) error {
	conn, err := amqp.Dial(p.amqpURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		userCreatedQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return ch.Publish(
		"",               // default exchange
		userCreatedQueue, // routing key = queue name
		false,            // mandatory
		false,            // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
}

// NoOpUserCreatedPublisher discards the event — used when RabbitMQ is not configured.
type NoOpUserCreatedPublisher struct{}

func (p *NoOpUserCreatedPublisher) Publish(_ UserCreatedEvent) error { return nil }

// PublishEmailEvent dials RabbitMQ, declares the durable queue, and publishes
// a single JSON-encoded EmailEvent. Connections and channels are closed via
// defer so resources are always released, even on error paths.
func PublishEmailEvent(amqpURL string, event EmailEvent) error {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	// Declare the queue as durable so messages survive a broker restart.
	_, err = ch.QueueDeclare(
		emailQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return ch.Publish(
		"",         // default exchange
		emailQueue, // routing key = queue name
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // survive broker restart
			Body:         body,
		},
	)
}
