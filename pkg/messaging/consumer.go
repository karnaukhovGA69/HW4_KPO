package messaging

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	conn   *amqp091.Connection
	queue  string
	logger *slog.Logger
}

func NewRabbitConsumer(url, exchange, queue string, logger *slog.Logger) (*Consumer, error) {
	conn, err := amqp091.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(
		exchange,
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("declare exchange: %w", err)
	}

	if _, err := ch.QueueDeclare(
		queue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("declare queue: %w", err)
	}

	if err := ch.QueueBind(
		queue,
		"",
		exchange,
		false,
		nil,
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("bind queue: %w", err)
	}

	return &Consumer{
		conn:   conn,
		queue:  queue,
		logger: logger,
	}, nil
}

func (c *Consumer) Start(ctx context.Context, handler func(context.Context, amqp091.Delivery)) error {
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}

	if err := ch.Qos(32, 0, false); err != nil {
		ch.Close()
		return fmt.Errorf("set qos: %w", err)
	}

	msgs, err := ch.Consume(c.queue, "", false, false, false, false, nil)
	if err != nil {
		ch.Close()
		return fmt.Errorf("consume queue: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = ch.Cancel("", false)
		ch.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-msgs:
			if !ok {
				if c.logger != nil {
					c.logger.Info("consumer channel closed")
				}
				return nil
			}
			handler(ctx, msg)
		}
	}
}

func (c *Consumer) Close() error {
	return c.conn.Close()
}
