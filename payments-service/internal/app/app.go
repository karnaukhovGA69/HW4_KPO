package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"gozon/payments-service/internal/account"
	"gozon/payments-service/internal/config"
	"gozon/payments-service/internal/httpapi"
	"gozon/payments-service/internal/payment"
	"gozon/payments-service/internal/storage"
	"gozon/pkg/contracts"
	"gozon/pkg/messaging"

	"github.com/rabbitmq/amqp091-go"
)

type App struct {
	cfg       config.Config
	logger    *slog.Logger
	store     *storage.Store
	accounts  *account.Service
	processor *payment.Processor
	publisher messaging.Publisher
	consumer  *messaging.Consumer
	outbox    *messaging.OutboxDispatcher
	httpSrv   *http.Server
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	store, err := storage.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	accounts := account.NewService(store.Pool())
	processor := payment.NewProcessor(store.Pool(), logger)

	publisher, err := messaging.NewRabbitPublisher(cfg.RabbitURL, cfg.PaymentsExchange)
	if err != nil {
		store.Close()
		return nil, err
	}

	consumer, err := messaging.NewRabbitConsumer(cfg.RabbitURL, cfg.OrdersExchange, cfg.OrdersQueue, logger)
	if err != nil {
		store.Close()
		publisher.Close()
		return nil, err
	}

	api := httpapi.NewServer(accounts, logger)
	httpSrv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: api,
	}

	outbox := messaging.NewOutboxDispatcher(store.Pool(), publisher, "payment_outbox", cfg.OutboxInterval, cfg.OutboxBatch, logger)

	return &App{
		cfg:       cfg,
		logger:    logger,
		store:     store,
		accounts:  accounts,
		processor: processor,
		publisher: publisher,
		consumer:  consumer,
		outbox:    outbox,
		httpSrv:   httpSrv,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	a.outbox.Start(ctx)

	go func() {
		errCh <- a.consumer.Start(ctx, a.handleOrderEvent)
	}()

	go func() {
		a.logger.Info("payments http server listening", "addr", a.cfg.HTTPAddr)
		if err := a.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (a *App) Close(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(ctx, a.cfg.ShutdownGracePeriod)
	defer cancel()
	_ = a.httpSrv.Shutdown(shutdownCtx)
	a.consumer.Close()
	a.publisher.Close()
	a.store.Close()
}

func (a *App) handleOrderEvent(ctx context.Context, msg amqp091.Delivery) {
	var evt contracts.OrderCreatedEvent
	if err := json.Unmarshal(msg.Body, &evt); err != nil {
		a.logger.Error("invalid order event", "err", err)
		_ = msg.Nack(false, false)
		return
	}

	if err := a.processor.HandleOrderCreated(ctx, evt); err != nil {
		a.logger.Error("process order event", "order_id", evt.OrderID, "err", err)
		_ = msg.Nack(false, true)
		return
	}

	_ = msg.Ack(false)
}

func Run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}
	defer app.Close(ctx)

	return app.Run(ctx)
}
