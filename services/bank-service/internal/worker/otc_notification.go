package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"banka-backend/services/bank-service/internal/domain"
	auth "banka-backend/shared/auth"

	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc/metadata"
)

// OTCNotifier sends email notifications for OTC offer lifecycle events.
// All methods are fire-and-forget; errors are logged but never returned.
type OTCNotifier interface {
	NotifyCounterOffer(offer domain.OTCOffer, callerID int64)
	NotifyOfferAccepted(offer domain.OTCOffer, contract domain.OTCContract)
	NotifyOfferDeclined(offer domain.OTCOffer, callerID int64)
	NotifyContractExpiringSoon(contract domain.OTCContractExpiringSoon, daysLeft int)
}

// AMQPOTCNotifier publishes OTC email events to the email_notifications queue.
type AMQPOTCNotifier struct {
	amqpURL   string
	resolver  UserEmailResolver
	jwtSecret string
}

func NewAMQPOTCNotifier(amqpURL string, resolver UserEmailResolver, jwtSecret string) *AMQPOTCNotifier {
	return &AMQPOTCNotifier{amqpURL: amqpURL, resolver: resolver, jwtSecret: jwtSecret}
}

func (n *AMQPOTCNotifier) serviceCtx() context.Context {
	token, err := auth.GenerateAccessToken("0", "bank-service@internal", "EMPLOYEE", []string{"SUPERVISOR"}, n.jwtSecret)
	if err != nil {
		return context.Background()
	}
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(context.Background(), md)
}

func (n *AMQPOTCNotifier) email(userID int64) string {
	e, err := n.resolver.ResolveEmail(n.serviceCtx(), userID)
	if err != nil {
		log.Printf("[otc-notif] email lookup user_id=%d: %v", userID, err)
		return ""
	}
	return e
}

// NotifyCounterOffer notifies the party that just received a counter-offer.
func (n *AMQPOTCNotifier) NotifyCounterOffer(offer domain.OTCOffer, callerID int64) {
	recipientID := offer.BuyerID
	if callerID == offer.BuyerID {
		recipientID = offer.SellerID
	}
	e := n.email(recipientID)
	if e == "" {
		return
	}
	n.publish(otcEmailEvent{
		Type:  "OTC_COUNTER_OFFER",
		Email: e,
		Data: map[string]string{
			"offer_id":        strconv.FormatInt(offer.ID, 10),
			"listing_id":      strconv.FormatInt(offer.ListingID, 10),
			"amount":          strconv.Itoa(int(offer.Amount)),
			"price_per_stock": fmt.Sprintf("%.2f", offer.PricePerStock),
			"premium":         fmt.Sprintf("%.2f", offer.Premium),
			"settlement_date": offer.SettlementDate.Format("2006-01-02"),
		},
	})
}

// NotifyOfferAccepted notifies both parties that the offer was accepted and a contract was created.
func (n *AMQPOTCNotifier) NotifyOfferAccepted(offer domain.OTCOffer, contract domain.OTCContract) {
	data := map[string]string{
		"offer_id":        strconv.FormatInt(offer.ID, 10),
		"contract_id":     strconv.FormatInt(contract.ID, 10),
		"listing_id":      strconv.FormatInt(offer.ListingID, 10),
		"amount":          strconv.Itoa(int(offer.Amount)),
		"strike_price":    fmt.Sprintf("%.2f", contract.StrikePrice),
		"premium":         fmt.Sprintf("%.2f", offer.Premium),
		"settlement_date": offer.SettlementDate.Format("2006-01-02"),
	}
	for _, recipientID := range []int64{offer.BuyerID, offer.SellerID} {
		e := n.email(recipientID)
		if e == "" {
			continue
		}
		n.publish(otcEmailEvent{Type: "OTC_OFFER_ACCEPTED", Email: e, Data: data})
	}
}

// NotifyOfferDeclined notifies the other party that the offer was declined or withdrawn.
func (n *AMQPOTCNotifier) NotifyOfferDeclined(offer domain.OTCOffer, callerID int64) {
	recipientID := offer.BuyerID
	if callerID == offer.BuyerID {
		recipientID = offer.SellerID
	}
	e := n.email(recipientID)
	if e == "" {
		return
	}
	n.publish(otcEmailEvent{
		Type:  "OTC_OFFER_DECLINED",
		Email: e,
		Data: map[string]string{
			"offer_id":   strconv.FormatInt(offer.ID, 10),
			"listing_id": strconv.FormatInt(offer.ListingID, 10),
			"status":     string(offer.Status),
		},
	})
}

// NotifyContractExpiringSoon notifies both buyer and seller that a contract is about to expire.
func (n *AMQPOTCNotifier) NotifyContractExpiringSoon(contract domain.OTCContractExpiringSoon, daysLeft int) {
	data := map[string]string{
		"contract_id":     strconv.FormatInt(contract.ID, 10),
		"listing_id":      strconv.FormatInt(contract.ListingID, 10),
		"ticker":          contract.Ticker,
		"settlement_date": contract.SettlementDate.Format("2006-01-02"),
		"days_left":       strconv.Itoa(daysLeft),
	}
	for _, recipientID := range []int64{contract.BuyerID, contract.SellerID} {
		e := n.email(recipientID)
		if e == "" {
			continue
		}
		n.publish(otcEmailEvent{Type: "OTC_CONTRACT_EXPIRING", Email: e, Data: data})
	}
}

type otcEmailEvent struct {
	Type  string            `json:"type"`
	Email string            `json:"email"`
	Token string            `json:"token"`
	Data  map[string]string `json:"data,omitempty"`
}

func (n *AMQPOTCNotifier) publish(event otcEmailEvent) {
	conn, err := amqp.Dial(n.amqpURL)
	if err != nil {
		log.Printf("[otc-notif] amqp dial: %v", err)
		return
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Printf("[otc-notif] amqp channel: %v", err)
		return
	}
	defer ch.Close()

	if _, err := ch.QueueDeclare(emailQueue, true, false, false, false, nil); err != nil {
		log.Printf("[otc-notif] queue declare: %v", err)
		return
	}

	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("[otc-notif] json marshal: %v", err)
		return
	}

	if err := ch.Publish("", emailQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}); err != nil {
		log.Printf("[otc-notif] publish type=%s: %v", event.Type, err)
		return
	}
	log.Printf("[otc-notif] published type=%s to %s", event.Type, event.Email)
}

// NoOpOTCNotifier discards all notifications (used when RabbitMQ is not configured).
type NoOpOTCNotifier struct{}

func (n *NoOpOTCNotifier) NotifyCounterOffer(offer domain.OTCOffer, callerID int64) {
	log.Printf("[otc-notif] noop OTC_COUNTER_OFFER offer_id=%d", offer.ID)
}
func (n *NoOpOTCNotifier) NotifyOfferAccepted(offer domain.OTCOffer, contract domain.OTCContract) {
	log.Printf("[otc-notif] noop OTC_OFFER_ACCEPTED offer_id=%d contract_id=%d", offer.ID, contract.ID)
}
func (n *NoOpOTCNotifier) NotifyOfferDeclined(offer domain.OTCOffer, callerID int64) {
	log.Printf("[otc-notif] noop OTC_OFFER_DECLINED offer_id=%d", offer.ID)
}
func (n *NoOpOTCNotifier) NotifyContractExpiringSoon(contract domain.OTCContractExpiringSoon, daysLeft int) {
	log.Printf("[otc-notif] noop OTC_CONTRACT_EXPIRING contract_id=%d days=%d", contract.ID, daysLeft)
}
