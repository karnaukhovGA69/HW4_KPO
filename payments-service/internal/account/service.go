package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAccountExists   = errors.New("account already exists")
	ErrAccountNotFound = errors.New("account not found")
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO accounts (user_id, balance, created_at, updated_at)
		VALUES ($1, 0, $2, $2)`,
		userID, now,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrAccountExists
		}
		return fmt.Errorf("create account: %w", err)
	}
	return nil
}

func (s *Service) Deposit(ctx context.Context, userID uuid.UUID, amount int64) (int64, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("amount must be positive")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE accounts
		SET balance = balance + $2, updated_at = NOW()
		WHERE user_id = $1`, userID, amount)
	if err != nil {
		return 0, fmt.Errorf("update balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, ErrAccountNotFound
	}

	txID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO account_transactions (id, user_id, amount, kind)
		VALUES ($1, $2, $3, $4)`,
		txID, userID, amount, "deposit",
	)
	if err != nil {
		return 0, fmt.Errorf("insert transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	balance, err := s.GetBalance(ctx, userID)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (s *Service) GetBalance(ctx context.Context, userID uuid.UUID) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx, `
		SELECT balance FROM accounts WHERE user_id = $1`,
		userID,
	).Scan(&balance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrAccountNotFound
		}
		return 0, fmt.Errorf("select balance: %w", err)
	}
	return balance, nil
}
