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
	OrdersQueue         string
	PaymentsExchange    string
	OutboxInterval      time.Duration
	OutboxBatch         int
	ShutdownGracePeriod time.Duration
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func Load() Config {
	return Config{
		HTTPAddr:            getEnv("PAYMENTS_HTTP_ADDR", ":8081"),
		DatabaseURL:         getEnv("PAYMENTS_DATABASE_URL", "postgres://payments:payments@payments-db:5432/payments?sslmode=disable"),
		RabbitURL:           getEnv("PAYMENTS_RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/"),
		OrdersExchange:      getEnv("ORDERS_EXCHANGE", "orders.events"),
		OrdersQueue:         getEnv("PAYMENTS_ORDERS_QUEUE", "payments.orders"),
		PaymentsExchange:    getEnv("PAYMENTS_EXCHANGE", "payments.events"),
		OutboxInterval:      parseDuration("PAYMENTS_OUTBOX_INTERVAL", 2*time.Second),
		OutboxBatch:         parseInt("PAYMENTS_OUTBOX_BATCH", 32),
		ShutdownGracePeriod: parseDuration("PAYMENTS_SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

func parseDuration(key string, def time.Duration) time.Duration {
	if raw, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(raw); err == nil {
			return d
		}
	}
	return def
}

func parseInt(key string, def int) int {
	if raw, ok := os.LookupEnv(key); ok {
		if v, err := strconv.Atoi(raw); err == nil {
			return v
		}
	}
	return def
}
