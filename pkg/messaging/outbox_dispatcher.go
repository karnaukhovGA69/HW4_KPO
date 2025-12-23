package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxDispatcher struct {
	pool      *pgxpool.Pool
	publisher Publisher
	table     string
	interval  time.Duration
	batchSize int
	logger    *slog.Logger
}

type outboxRow struct {
	ID        int64
	EventType string
	Payload   []byte
	Attempts  int
}

func NewOutboxDispatcher(pool *pgxpool.Pool, publisher Publisher, table string, interval time.Duration, batch int, logger *slog.Logger) *OutboxDispatcher {
	return &OutboxDispatcher{
		pool:      pool,
		publisher: publisher,
		table:     table,
		interval:  interval,
		batchSize: batch,
		logger:    logger,
	}
}

func (d *OutboxDispatcher) Start(ctx context.Context) {
	go d.loop(ctx)
}

func (d *OutboxDispatcher) loop(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		if err := d.dispatch(ctx); err != nil {
			d.logger.Error("outbox dispatch failed", "table", d.table, "err", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *OutboxDispatcher) dispatch(ctx context.Context) error {
	rows, err := d.lockRows(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	for _, row := range rows {
		if err := d.publishOne(ctx, row); err != nil {
			d.logger.Warn("publish event failed", "table", d.table, "row_id", row.ID, "err", err)
		}
	}
	return nil
}

func (d *OutboxDispatcher) lockRows(ctx context.Context) ([]outboxRow, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	query := fmt.Sprintf(`
		SELECT id, event_type, payload, attempts
		FROM %s
		WHERE (status = 'pending' OR (status = 'processing' AND next_retry <= NOW()))
		ORDER BY id
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, d.table)

	rows, err := tx.Query(ctx, query, d.batchSize)
	if err != nil {
		return nil, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	var items []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.ID, &row.EventType, &row.Payload, &row.Attempts); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	releaseAt := time.Now().Add(30 * time.Second)
	for _, row := range items {
		updateQuery := fmt.Sprintf(`
			UPDATE %s
			SET status = 'processing', next_retry = $2, updated_at = NOW()
			WHERE id = $1`, d.table)
		if _, err := tx.Exec(ctx, updateQuery, row.ID, releaseAt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}

func (d *OutboxDispatcher) publishOne(ctx context.Context, row outboxRow) error {
	pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := d.publisher.Publish(pubCtx, "", row.Payload); err != nil {
		return d.markFailure(ctx, row, err)
	}

	update := fmt.Sprintf(`
		UPDATE %s
		SET status = 'sent', updated_at = NOW()
		WHERE id = $1`, d.table)
	_, err := d.pool.Exec(ctx, update, row.ID)
	return err
}

func (d *OutboxDispatcher) markFailure(ctx context.Context, row outboxRow, publishErr error) error {
	delay := retryDelay(row.Attempts + 1)
	nextRetry := time.Now().Add(delay)
	query := fmt.Sprintf(`
		UPDATE %s
		SET status = 'pending',
		    attempts = attempts + 1,
	    next_retry = $2,
		    updated_at = NOW()
		WHERE id = $1`, d.table)
	if _, err := d.pool.Exec(ctx, query, row.ID, nextRetry); err != nil {
		return fmt.Errorf("update retry: %w", err)
	}
	return publishErr
}

func retryDelay(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	if attempts > 5 {
		attempts = 5
	}
	delay := time.Duration(1<<attempts) * time.Second
	if delay > time.Minute {
		delay = time.Minute
	}
	return delay
}
