package contracts

import "time"

type OrderCreatedEvent struct {
	EventID   string    `json:"event_id"`
	OrderID   string    `json:"order_id"`
	UserID    string    `json:"user_id"`
	Amount    int64     `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

type PaymentStatus string

const (
	PaymentSucceeded PaymentStatus = "succeeded"
	PaymentFailed    PaymentStatus = "failed"
)

type PaymentProcessedEvent struct {
	EventID   string        `json:"event_id"`
	OrderID   string        `json:"order_id"`
	UserID    string        `json:"user_id"`
	Amount    int64         `json:"amount"`
	Status    PaymentStatus `json:"status"`
	Reason    string        `json:"reason,omitempty"`
	Processed time.Time     `json:"processed_at"`
}
