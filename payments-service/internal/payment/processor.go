package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gozon/pkg/contracts"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Status string

const (
	StatusProcessing Status = "processing"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
)

type Processor struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewProcessor(pool *pgxpool.Pool, logger *slog.Logger) *Processor {
	return &Processor{
		pool:   pool,
		logger: logger,
	}
}

func (p *Processor) HandleOrderCreated(ctx context.Context, evt contracts.OrderCreatedEvent) error {
	userID, err := uuid.Parse(evt.UserID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	orderID, err := uuid.Parse(evt.OrderID)
	if err != nil {
		return fmt.Errorf("invalid order id: %w", err)
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO payment_inbox (event_id, event_type)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`,
		evt.EventID, "orders.created",
	)
	if err != nil {
		return fmt.Errorf("insert inbox: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO payments (order_id, user_id, amount, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (order_id) DO NOTHING`,
		orderID, userID, evt.Amount, StatusProcessing,
	)
	if err != nil {
		return fmt.Errorf("insert payment row: %w", err)
	}

	status := StatusFailed
	reason := ""
	success := false

	var balance int64
	err = tx.QueryRow(ctx, `
		SELECT balance
		FROM accounts
		WHERE user_id = $1
		FOR UPDATE`,
		userID,
	).Scan(&balance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			reason = "account_missing"
		} else {
			return fmt.Errorf("select balance: %w", err)
		}
	} else if balance < evt.Amount {
		reason = "insufficient_funds"
	} else {
		tag, err := tx.Exec(ctx, `
			UPDATE accounts
			SET balance = balance - $2, updated_at = NOW()
			WHERE user_id = $1`, userID, evt.Amount)
		if err != nil {
			return fmt.Errorf("deduct balance: %w", err)
		}
		if tag.RowsAffected() == 0 {
			reason = "account_missing"
		} else {
			success = true
			status = StatusSucceeded
			reason = ""
			txID := uuid.New()
			_, err = tx.Exec(ctx, `
				INSERT INTO account_transactions (id, user_id, order_id, amount, kind)
				VALUES ($1, $2, $3, $4, $5)`,
				txID, userID, orderID, evt.Amount, "debit",
			)
			if err != nil {
				return fmt.Errorf("insert account transaction: %w", err)
			}
		}
	}

	if !success {
		status = StatusFailed
		if reason == "" {
			reason = "unknown_error"
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE payments
		SET status = $2, reason = $3, updated_at = NOW()
		WHERE order_id = $1`,
		orderID, status, reason,
	)
	if err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}

	result := contracts.PaymentProcessedEvent{
		EventID:   uuid.New().String(),
		OrderID:   evt.OrderID,
		UserID:    evt.UserID,
		Amount:    evt.Amount,
		Status:    contracts.PaymentFailed,
		Reason:    reason,
		Processed: time.Now().UTC(),
	}
	if success {
		result.Status = contracts.PaymentSucceeded
		result.Reason = ""
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal payment event: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO payment_outbox (event_id, event_type, payload)
		VALUES ($1, $2, $3)`,
		result.EventID, "payments.processed", payload,
	)
	if err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}

	return tx.Commit(ctx)
}
