package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"banka-backend/services/bank-service/internal/trading"
	auth "banka-backend/shared/auth"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/metadata"
)

// OrderNotifier is the interface used by BankHandler and the trading engine
// to send order lifecycle email notifications.
// All methods are fire-and-forget; implementations handle their own goroutines.
type OrderNotifier interface {
	NotifyOrderPending(order trading.Order, ticker string)
	NotifyOrderApproved(order trading.Order, ticker string)
	NotifyOrderDeclined(order trading.Order, ticker string)
	NotifyOrderCanceled(order trading.Order, ticker string)
	NotifyOrderExecuted(order trading.Order, ticker string, filledQty int32, executedPrice decimal.Decimal)
}

// OrderEmailEvent is the AMQP payload for order-related email notifications.
// JSON field names match notification-service domain.EmailEvent.
type OrderEmailEvent struct {
	Type  string            `json:"type"`
	Email string            `json:"email"`
	Token string            `json:"token"`
	Data  map[string]string `json:"data,omitempty"`
}

// UserEmailResolver resolves a user's email by their ID.
// The concrete implementation calls user-service via gRPC with a service token.
type UserEmailResolver interface {
	ResolveEmail(ctx context.Context, userID int64) (string, error)
}

// OrderNotificationPublisher sends order lifecycle email events via RabbitMQ.
type OrderNotificationPublisher struct {
	amqpURL    string
	resolver   UserEmailResolver
	jwtSecret  string
}

func NewOrderNotificationPublisher(amqpURL string, resolver UserEmailResolver, jwtSecret string) *OrderNotificationPublisher {
	return &OrderNotificationPublisher{amqpURL: amqpURL, resolver: resolver, jwtSecret: jwtSecret}
}

// serviceCtx returns a background context carrying a service-account JWT so
// user-service gRPC calls succeed without a client token.
func (p *OrderNotificationPublisher) serviceCtx() context.Context {
	token, err := auth.GenerateAccessToken("0", "bank-service@internal", "EMPLOYEE", []string{"SUPERVISOR"}, p.jwtSecret)
	if err != nil {
		return context.Background()
	}
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(context.Background(), md)
}

func (p *OrderNotificationPublisher) resolveEmail(ctx context.Context, userID int64) string {
	email, err := p.resolver.ResolveEmail(ctx, userID)
	if err != nil {
		log.Printf("[order-notif] email lookup için user_id=%d: %v", userID, err)
		return ""
	}
	return email
}

// NotifyOrderPending sends ORDER_PENDING when an agent's order awaits supervisor approval.
func (p *OrderNotificationPublisher) NotifyOrderPending(order trading.Order, ticker string) {
	email := p.resolveEmail(p.serviceCtx(), order.UserID)
	if email == "" {
		return
	}
	p.publish(OrderEmailEvent{
		Type:  "ORDER_PENDING",
		Email: email,
		Data:  orderData(order, ticker, 0, decimal.Zero, 0),
	})
}

// NotifyOrderApproved sends ORDER_APPROVED to the order owner.
func (p *OrderNotificationPublisher) NotifyOrderApproved(order trading.Order, ticker string) {
	email := p.resolveEmail(p.serviceCtx(), order.UserID)
	if email == "" {
		return
	}
	p.publish(OrderEmailEvent{
		Type:  "ORDER_APPROVED",
		Email: email,
		Data:  orderData(order, ticker, 0, decimal.Zero, 0),
	})
}

// NotifyOrderDeclined sends ORDER_DECLINED to the order owner.
func (p *OrderNotificationPublisher) NotifyOrderDeclined(order trading.Order, ticker string) {
	email := p.resolveEmail(p.serviceCtx(), order.UserID)
	if email == "" {
		return
	}
	p.publish(OrderEmailEvent{
		Type:  "ORDER_DECLINED",
		Email: email,
		Data:  orderData(order, ticker, 0, decimal.Zero, 0),
	})
}

// NotifyOrderCanceled sends ORDER_CANCELED to the order owner.
func (p *OrderNotificationPublisher) NotifyOrderCanceled(order trading.Order, ticker string) {
	email := p.resolveEmail(p.serviceCtx(), order.UserID)
	if email == "" {
		return
	}
	p.publish(OrderEmailEvent{
		Type:  "ORDER_CANCELED",
		Email: email,
		Data:  orderData(order, ticker, 0, decimal.Zero, 0),
	})
}

// NotifyOrderExecuted sends ORDER_EXECUTED when a fill occurs (partial or full).
func (p *OrderNotificationPublisher) NotifyOrderExecuted(order trading.Order, ticker string, filledQty int32, executedPrice decimal.Decimal) {
	email := p.resolveEmail(p.serviceCtx(), order.UserID)
	if email == "" {
		return
	}
	p.publish(OrderEmailEvent{
		Type:  "ORDER_EXECUTED",
		Email: email,
		Data:  orderData(order, ticker, filledQty, executedPrice, order.RemainingPortions),
	})
}

func orderData(order trading.Order, ticker string, filledQty int32, executedPrice decimal.Decimal, remaining int32) map[string]string {
	d := map[string]string{
		"order_id":   strconv.FormatInt(order.ID, 10),
		"ticker":     ticker,
		"direction":  string(order.Direction),
		"quantity":   strconv.Itoa(int(order.Quantity)),
		"order_type": string(order.OrderType),
	}
	if filledQty > 0 {
		d["filled_qty"] = strconv.Itoa(int(filledQty))
		d["executed_price"] = executedPrice.String()
		d["remaining"] = strconv.Itoa(int(remaining))
		if remaining == 0 {
			d["execution_type"] = "potpuno izvršen"
			d["execution_label"] = "potpuno izvršen"
		} else {
			d["execution_type"] = "delimično izvršen"
			d["execution_label"] = "delimično izvršen"
		}
	}
	return d
}

func (p *OrderNotificationPublisher) publish(event OrderEmailEvent) {
	conn, err := amqp.Dial(p.amqpURL)
	if err != nil {
		log.Printf("[order-notif] amqp dial: %v", err)
		return
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Printf("[order-notif] amqp channel: %v", err)
		return
	}
	defer ch.Close()

	if _, err := ch.QueueDeclare(emailQueue, true, false, false, false, nil); err != nil {
		log.Printf("[order-notif] queue declare: %v", err)
		return
	}

	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("[order-notif] json marshal: %v", err)
		return
	}

	if err := ch.Publish("", emailQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}); err != nil {
		log.Printf("[order-notif] publish type=%s: %v", event.Type, err)
		return
	}
	log.Printf("[order-notif] published type=%s to %s", event.Type, event.Email)
}

// NoOpOrderNotificationPublisher discards all notifications (used when RabbitMQ is not configured).
type NoOpOrderNotificationPublisher struct{}

func (p *NoOpOrderNotificationPublisher) NotifyOrderPending(order trading.Order, ticker string) {
	log.Printf("[order-notif] noop ORDER_PENDING order_id=%d ticker=%s", order.ID, ticker)
}
func (p *NoOpOrderNotificationPublisher) NotifyOrderApproved(order trading.Order, ticker string) {
	log.Printf("[order-notif] noop ORDER_APPROVED order_id=%d ticker=%s", order.ID, ticker)
}
func (p *NoOpOrderNotificationPublisher) NotifyOrderDeclined(order trading.Order, ticker string) {
	log.Printf("[order-notif] noop ORDER_DECLINED order_id=%d ticker=%s", order.ID, ticker)
}
func (p *NoOpOrderNotificationPublisher) NotifyOrderCanceled(order trading.Order, ticker string) {
	log.Printf("[order-notif] noop ORDER_CANCELED order_id=%d ticker=%s", order.ID, ticker)
}
func (p *NoOpOrderNotificationPublisher) NotifyOrderExecuted(order trading.Order, ticker string, filledQty int32, executedPrice decimal.Decimal) {
	log.Printf("[order-notif] noop ORDER_EXECUTED order_id=%d ticker=%s qty=%d", order.ID, ticker, filledQty)
}

// userServiceEmailResolver adapts transport.UserServiceClient to UserEmailResolver.
// It is defined here to keep the worker package self-contained; the concrete
// client is injected from main.go.
type userServiceEmailResolver struct {
	getEmail func(ctx context.Context, userID int64) (string, error)
}

// NewUserServiceEmailResolver wraps a GetClientEmail func (e.g. from transport.UserServiceClient).
func NewUserServiceEmailResolver(getEmail func(ctx context.Context, userID int64) (string, error)) UserEmailResolver {
	return &userServiceEmailResolver{getEmail: getEmail}
}

func (r *userServiceEmailResolver) ResolveEmail(ctx context.Context, userID int64) (string, error) {
	email, err := r.getEmail(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("resolve email for user %d: %w", userID, err)
	}
	return email, nil
}
