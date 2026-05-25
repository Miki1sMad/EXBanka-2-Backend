package worker

// otc_notification_db.go — in-app dispatch OTC notifikacija kroz FCMDispatcher.
//
// CompositeOTCNotifier omogućava da bank-service istovremeno:
//   • šalje email kroz postojeći AMQPOTCNotifier (RabbitMQ)
//   • upisuje u core_banking.push_notifications (in-app inbox) + šalje FCM push
//
// Ne menja postojeći AMQPOTCNotifier — samo ga wrap-uje.

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"banka-backend/services/bank-service/internal/domain"
)

// DBOTCNotifier implementira OTCNotifier upisom u push_notifications i FCM-om.
type DBOTCNotifier struct {
	dispatcher *FCMDispatcher
}

// NewDBOTCNotifier konstruktor.
func NewDBOTCNotifier(dispatcher *FCMDispatcher) *DBOTCNotifier {
	return &DBOTCNotifier{dispatcher: dispatcher}
}

func (n *DBOTCNotifier) NotifyCounterOffer(offer domain.OTCOffer, callerID int64) {
	recipientID := offer.BuyerID
	if callerID == offer.BuyerID {
		recipientID = offer.SellerID
	}
	title := "Nova OTC kontraponuda"
	body := fmt.Sprintf("Akcija %d • %d kom × %.2f USD (premija %.2f)",
		offer.ListingID, offer.Amount, offer.PricePerStock, offer.Premium)
	data := map[string]string{
		"offer_id":        strconv.FormatInt(offer.ID, 10),
		"listing_id":      strconv.FormatInt(offer.ListingID, 10),
		"amount":          strconv.Itoa(int(offer.Amount)),
		"price_per_stock": fmt.Sprintf("%.2f", offer.PricePerStock),
		"premium":         fmt.Sprintf("%.2f", offer.Premium),
		"settlement_date": offer.SettlementDate.Format("2006-01-02"),
	}
	if _, err := n.dispatcher.Notify(context.Background(), recipientID, "OTC_COUNTER_OFFER", title, body, data); err != nil {
		log.Printf("[otc-db-notif] counter offer inbox failed user=%d: %v", recipientID, err)
	}
}

func (n *DBOTCNotifier) NotifyOfferAccepted(offer domain.OTCOffer, contract domain.OTCContract) {
	title := "OTC ponuda prihvaćena"
	body := fmt.Sprintf("Ugovor #%d • %d kom • settlement %s",
		contract.ID, offer.Amount, offer.SettlementDate.Format("02.01.2006"))
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
		if _, err := n.dispatcher.Notify(context.Background(), recipientID, "OTC_ACCEPTED", title, body, data); err != nil {
			log.Printf("[otc-db-notif] accepted inbox failed user=%d: %v", recipientID, err)
		}
	}
}

func (n *DBOTCNotifier) NotifyOfferDeclined(offer domain.OTCOffer, callerID int64) {
	recipientID := offer.BuyerID
	if callerID == offer.BuyerID {
		recipientID = offer.SellerID
	}
	title := "OTC ponuda odbijena"
	body := fmt.Sprintf("Akcija %d • %d kom × %.2f USD",
		offer.ListingID, offer.Amount, offer.PricePerStock)
	data := map[string]string{
		"offer_id":   strconv.FormatInt(offer.ID, 10),
		"listing_id": strconv.FormatInt(offer.ListingID, 10),
		"amount":     strconv.Itoa(int(offer.Amount)),
	}
	if _, err := n.dispatcher.Notify(context.Background(), recipientID, "OTC_DECLINED", title, body, data); err != nil {
		log.Printf("[otc-db-notif] declined inbox failed user=%d: %v", recipientID, err)
	}
}

func (n *DBOTCNotifier) NotifyContractExpiringSoon(contract domain.OTCContractExpiringSoon, daysLeft int) {
	title := "OTC ugovor uskoro ističe"
	body := fmt.Sprintf("Ugovor #%d ističe za %d dana", contract.ID, daysLeft)
	data := map[string]string{
		"contract_id": strconv.FormatInt(contract.ID, 10),
		"days_left":   strconv.Itoa(daysLeft),
	}
	for _, recipientID := range []int64{contract.BuyerID, contract.SellerID} {
		if recipientID == 0 {
			continue
		}
		if _, err := n.dispatcher.Notify(context.Background(), recipientID, "OTC_CONTRACT_EXPIRING", title, body, data); err != nil {
			log.Printf("[otc-db-notif] expiring inbox failed user=%d: %v", recipientID, err)
		}
	}
}

// CompositeOTCNotifier kombinuje više OTCNotifier-a (npr. AMQP + DB) tako da
// svaki događaj putuje kroz oba kanala.
type CompositeOTCNotifier struct {
	delegates []OTCNotifier
}

// NewCompositeOTCNotifier wraps any number of notifiers. nil delegate-i se preskoče.
func NewCompositeOTCNotifier(delegates ...OTCNotifier) *CompositeOTCNotifier {
	out := &CompositeOTCNotifier{}
	for _, d := range delegates {
		if d != nil {
			out.delegates = append(out.delegates, d)
		}
	}
	return out
}

func (c *CompositeOTCNotifier) NotifyCounterOffer(offer domain.OTCOffer, callerID int64) {
	for _, d := range c.delegates {
		d.NotifyCounterOffer(offer, callerID)
	}
}

func (c *CompositeOTCNotifier) NotifyOfferAccepted(offer domain.OTCOffer, contract domain.OTCContract) {
	for _, d := range c.delegates {
		d.NotifyOfferAccepted(offer, contract)
	}
}

func (c *CompositeOTCNotifier) NotifyOfferDeclined(offer domain.OTCOffer, callerID int64) {
	for _, d := range c.delegates {
		d.NotifyOfferDeclined(offer, callerID)
	}
}

func (c *CompositeOTCNotifier) NotifyContractExpiringSoon(contract domain.OTCContractExpiringSoon, daysLeft int) {
	for _, d := range c.delegates {
		d.NotifyContractExpiringSoon(contract, daysLeft)
	}
}
