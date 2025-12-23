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

	"gozon/orders-service/internal/config"
	"gozon/orders-service/internal/httpapi"
	"gozon/orders-service/internal/order"
	"gozon/orders-service/internal/storage"
	"gozon/pkg/contracts"
	"gozon/pkg/messaging"

	"github.com/rabbitmq/amqp091-go"
)

type App struct {
	cfg       config.Config
	logger    *slog.Logger
	store     *storage.Store
	orderSvc  *order.Service
	publisher messaging.Publisher
	outbox    *messaging.OutboxDispatcher
	consumer  *messaging.Consumer
	httpSrv   *http.Server
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	store, err := storage.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	orderSvc := order.NewService(store.Pool())

	publisher, err := messaging.NewRabbitPublisher(cfg.RabbitURL, cfg.OrdersExchange)
	if err != nil {
		store.Close()
		return nil, err
	}

	consumer, err := messaging.NewRabbitConsumer(cfg.RabbitURL, cfg.PaymentsExchange, cfg.PaymentsQueue, logger)
	if err != nil {
		store.Close()
		publisher.Close()
		return nil, err
	}

	api := httpapi.NewServer(orderSvc, logger)
	httpSrv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: api,
	}

	outbox := messaging.NewOutboxDispatcher(store.Pool(), publisher, "order_outbox", cfg.OutboxInterval, cfg.OutboxBatchSize, logger)

	return &App{
		cfg:       cfg,
		logger:    logger,
		store:     store,
		orderSvc:  orderSvc,
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
		errCh <- a.consumer.Start(ctx, a.handlePaymentMessage)
	}()

	go func() {
		a.logger.Info("orders http server listening", "addr", a.cfg.HTTPAddr)
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

func (a *App) handlePaymentMessage(ctx context.Context, msg amqp091.Delivery) {
	var evt contracts.PaymentProcessedEvent
	if err := json.Unmarshal(msg.Body, &evt); err != nil {
		a.logger.Error("invalid payment event", "err", err)
		_ = msg.Nack(false, false)
		return
	}

	if err := a.orderSvc.ApplyPaymentResult(ctx, evt); err != nil {
		a.logger.Error("apply payment result failed", "order_id", evt.OrderID, "err", err)
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

	if err := app.Run(ctx); err != nil {
		return err
	}

	return nil
}
