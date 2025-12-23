package contracts

import "time"

// OrderCreatedEvent describes a newly created order that needs to be charged.
type OrderCreatedEvent struct {
	EventID   string    `json:"event_id"`
	OrderID   string    `json:"order_id"`
	UserID    string    `json:"user_id"`
	Amount    int64     `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

// PaymentStatus shows how the payment attempt finished.
type PaymentStatus string

const (
	PaymentSucceeded PaymentStatus = "succeeded"
	PaymentFailed    PaymentStatus = "failed"
)

// PaymentProcessedEvent is emitted by the Payments Service when it finishes processing an order.
type PaymentProcessedEvent struct {
	EventID   string        `json:"event_id"`
	OrderID   string        `json:"order_id"`
	UserID    string        `json:"user_id"`
	Amount    int64         `json:"amount"`
	Status    PaymentStatus `json:"status"`
	Reason    string        `json:"reason,omitempty"`
	Processed time.Time     `json:"processed_at"`
}
