package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gozon/pkg/contracts"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrOrderNotFound = errors.New("order not found")
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, amount int64) (*Order, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	orderID := uuid.New()
	order := &Order{
		ID:        orderID.String(),
		UserID:    userID.String(),
		Amount:    amount,
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO orders (id, user_id, amount, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		orderID, userID, amount, StatusPending, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert order: %w", err)
	}

	event := contracts.OrderCreatedEvent{
		EventID:   uuid.New().String(),
		OrderID:   orderID.String(),
		UserID:    userID.String(),
		Amount:    amount,
		CreatedAt: now,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO order_outbox (event_id, event_type, payload)
		VALUES ($1, $2, $3)`,
		event.EventID, "orders.created", payload,
	)
	if err != nil {
		return nil, fmt.Errorf("insert outbox: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return order, nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, amount, status, created_at, updated_at
		FROM orders
		WHERE user_id = $1
		ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query orders: %w", err)
	}
	defer rows.Close()

	var result []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.Amount, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, o)
	}

	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, userID uuid.UUID, orderID uuid.UUID) (*Order, error) {
	var o Order
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, amount, status, created_at, updated_at
		FROM orders
		WHERE id = $1 AND user_id = $2`,
		orderID, userID,
	).Scan(&o.ID, &o.UserID, &o.Amount, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("get order: %w", err)
	}
	return &o, nil
}

func (s *Service) ApplyPaymentResult(ctx context.Context, evt contracts.PaymentProcessedEvent) error {
	eventID, err := uuid.Parse(evt.EventID)
	if err != nil {
		return fmt.Errorf("invalid event id: %w", err)
	}
	orderID, err := uuid.Parse(evt.OrderID)
	if err != nil {
		return fmt.Errorf("invalid order id: %w", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO order_inbox (event_id, event_type)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`,
		eventID, "payments.processed")
	if err != nil {
		return fmt.Errorf("insert inbox: %w", err)
	}

	if tag.RowsAffected() == 0 {
		// Already processed.
		return nil
	}

	var status Status
	switch evt.Status {
	case contracts.PaymentSucceeded:
		status = StatusPaid
	default:
		status = StatusFailed
	}

	tag, err = tx.Exec(ctx, `
		UPDATE orders
		SET status = $2, updated_at = NOW()
		WHERE id = $1`,
		orderID, status,
	)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOrderNotFound
	}

	return tx.Commit(ctx)
}
