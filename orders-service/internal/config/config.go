package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr            string
	DatabaseURL         string
	RabbitURL           string
	OrdersExchange      string
	PaymentsExchange    string
	PaymentsQueue       string
	OutboxInterval      time.Duration
	OutboxBatchSize     int
	ShutdownGracePeriod time.Duration
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func Load() Config {
	httpAddr := getEnv("ORDERS_HTTP_ADDR", ":8080")
	dbURL := getEnv("ORDERS_DATABASE_URL", "postgres://orders:orders@orders-db:5432/orders?sslmode=disable")
	rabbit := getEnv("ORDERS_RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/")
	ordersExchange := getEnv("ORDERS_EXCHANGE", "orders.events")
	paymentsExchange := getEnv("PAYMENTS_EXCHANGE", "payments.events")
	paymentsQueue := getEnv("ORDERS_PAYMENTS_QUEUE", "orders.payment-results")

	outboxInterval := parseDuration("ORDERS_OUTBOX_INTERVAL", 2*time.Second)
	outboxBatch := parseInt("ORDERS_OUTBOX_BATCH", 32)
	grace := parseDuration("ORDERS_SHUTDOWN_TIMEOUT", 10*time.Second)

	return Config{
		HTTPAddr:            httpAddr,
		DatabaseURL:         dbURL,
		RabbitURL:           rabbit,
		OrdersExchange:      ordersExchange,
		PaymentsExchange:    paymentsExchange,
		PaymentsQueue:       paymentsQueue,
		OutboxInterval:      outboxInterval,
		OutboxBatchSize:     outboxBatch,
		ShutdownGracePeriod: grace,
	}
}

func parseDuration(key string, def time.Duration) time.Duration {
	raw := getEnv(key, "")
	if raw == "" {
		return def
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	return def
}

func parseInt(key string, def int) int {
	raw := getEnv(key, "")
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil {
		return v
	}
	return def
}
