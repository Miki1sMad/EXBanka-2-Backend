package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/trading"

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
)

// RecurringOrderWorker fires scheduled market orders (DCA) on a per-minute tick.
type RecurringOrderWorker struct {
	repo           domain.RecurringOrderRepository
	tradingService trading.TradingService
	db             *gorm.DB
	amqpURL        string
	emailResolver  UserEmailResolver
}

func NewRecurringOrderWorker(
	repo domain.RecurringOrderRepository,
	tradingService trading.TradingService,
	db *gorm.DB,
	amqpURL string,
	emailResolver UserEmailResolver,
) *RecurringOrderWorker {
	return &RecurringOrderWorker{
		repo:           repo,
		tradingService: tradingService,
		db:             db,
		amqpURL:        amqpURL,
		emailResolver:  emailResolver,
	}
}

// Start runs the worker until ctx is cancelled, firing once per minute.
func (w *RecurringOrderWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	log.Println("[recurring-order] worker started")
	for {
		select {
		case <-ctx.Done():
			log.Println("[recurring-order] worker stopped")
			return
		case <-ticker.C:
			w.runCycle(ctx)
		}
	}
}

func (w *RecurringOrderWorker) runCycle(ctx context.Context) {
	orders, err := w.repo.ListDue(time.Now())
	if err != nil {
		log.Printf("[recurring-order] list due: %v", err)
		return
	}
	for _, order := range orders {
		w.processOne(ctx, order)
	}
}

func (w *RecurringOrderWorker) processOne(ctx context.Context, order domain.RecurringOrder) {
	// 1. Get ask price for the listing.
	var ask float64
	if err := w.db.WithContext(ctx).Raw(
		"SELECT ask FROM core_banking.listing WHERE id = ?", order.ListingID,
	).Scan(&ask).Error; err != nil || ask <= 0 {
		log.Printf("[recurring-order] listing %d ask price unavailable: %v", order.ListingID, err)
		w.advanceOrder(order)
		return
	}

	// Get ticker for notifications.
	var ticker string
	w.db.WithContext(ctx).Raw("SELECT ticker FROM core_banking.listing WHERE id = ?", order.ListingID).Scan(&ticker)

	// 2. Compute quantity and estimated cost.
	var quantity int32
	var estimatedCost float64

	switch order.Mode {
	case "BYAMOUNT":
		q := math.Floor(order.Value / ask)
		if q < 1 {
			log.Printf("[recurring-order] order %d: insufficient value for 1 unit (ask=%.4f value=%.4f), skipping", order.ID, ask, order.Value)
			w.advanceAndNotify(ctx, order, ticker)
			return
		}
		quantity = int32(q)
		estimatedCost = order.Value
	case "BYQUANTITY":
		quantity = int32(order.Value)
		estimatedCost = float64(quantity) * ask
	default:
		log.Printf("[recurring-order] order %d: unknown mode %q, skipping", order.ID, order.Mode)
		w.advanceOrder(order)
		return
	}

	// 3. Check account balance.
	var free float64
	if err := w.db.WithContext(ctx).Raw(
		"SELECT stanje_racuna - COALESCE(rezervisana_sredstva, 0) AS free FROM core_banking.racun WHERE id = ?",
		order.AccountID,
	).Scan(&free).Error; err != nil {
		log.Printf("[recurring-order] order %d: balance check failed: %v", order.ID, err)
		w.advanceAndNotify(ctx, order, ticker)
		return
	}

	if free < estimatedCost {
		log.Printf("[recurring-order] order %d: insufficient funds (free=%.2f needed=%.2f), skipping", order.ID, free, estimatedCost)
		w.advanceAndNotify(ctx, order, ticker)
		return
	}

	// 4. Submit the market order.
	_, err := w.tradingService.CreateOrder(ctx, &trading.CreateOrderRequest{
		UserID:       order.UserID,
		AccountID:    order.AccountID,
		ListingID:    order.ListingID,
		OrderType:    trading.OrderTypeMarket,
		Direction:    trading.OrderDirection(order.Direction),
		Quantity:     quantity,
		ContractSize: 1,
		IsClient:     order.IsClient,
		IsSupervisor: false,
		AfterHours:   true,
		AllOrNone:    false,
		Margin:       false,
	})
	if err != nil {
		log.Printf("[recurring-order] order %d: CreateOrder failed: %v", order.ID, err)
		if err == trading.ErrInsufficientFunds {
			w.advanceAndNotify(ctx, order, ticker)
			return
		}
		// For other errors, still advance to avoid infinite retry on bad data.
		w.advanceOrder(order)
		return
	}

	log.Printf("[recurring-order] order %d: fired market order qty=%d for listing=%d", order.ID, quantity, order.ListingID)
	w.advanceOrder(order)
}

func (w *RecurringOrderWorker) advanceAndNotify(ctx context.Context, order domain.RecurringOrder, ticker string) {
	advanced := advanceNextRun(order.NextRun, order.Cadence)
	order.NextRun = advanced
	if err := w.repo.Update(order); err != nil {
		log.Printf("[recurring-order] update order %d: %v", order.ID, err)
	}

	email, err := w.emailResolver.ResolveEmail(ctx, order.UserID)
	if err != nil || email == "" {
		log.Printf("[recurring-order] order %d: could not resolve email for user %d: %v", order.ID, order.UserID, err)
		return
	}

	event := OrderEmailEvent{
		Type:  "RECURRING_ORDER_SKIPPED",
		Email: email,
		Data: map[string]string{
			"ticker":   ticker,
			"value":    fmt.Sprintf("%.2f", order.Value),
			"mode":     order.Mode,
			"next_run": advanced.Format("2006-01-02"),
		},
	}
	if err := w.publishEvent(event); err != nil {
		log.Printf("[recurring-order] publish skip notification order %d: %v", order.ID, err)
	}
}

func (w *RecurringOrderWorker) advanceOrder(order domain.RecurringOrder) {
	order.NextRun = advanceNextRun(order.NextRun, order.Cadence)
	if err := w.repo.Update(order); err != nil {
		log.Printf("[recurring-order] update order %d: %v", order.ID, err)
	}
}

func advanceNextRun(t time.Time, cadence string) time.Time {
	switch cadence {
	case "DAILY":
		return t.AddDate(0, 0, 1)
	case "WEEKLY":
		return t.AddDate(0, 0, 7)
	case "MONTHLY":
		return t.AddDate(0, 1, 0)
	default:
		return t.AddDate(0, 0, 1)
	}
}

func (w *RecurringOrderWorker) publishEvent(event OrderEmailEvent) error {
	if w.amqpURL == "" {
		log.Printf("[recurring-order] noop — RabbitMQ nije konfigurisan (RECURRING_ORDER_SKIPPED za %s)", event.Email)
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
