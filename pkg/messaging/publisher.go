package messaging

import (
	"context"
	"fmt"

	"github.com/rabbitmq/amqp091-go"
)

type Publisher interface {
	Publish(ctx context.Context, routingKey string, payload []byte) error
	Close() error
}

type RabbitPublisher struct {
	conn     *amqp091.Connection
	exchange string
}

func NewRabbitPublisher(url, exchange string) (*RabbitPublisher, error) {
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

	return &RabbitPublisher{conn: conn, exchange: exchange}, nil
}

func (p *RabbitPublisher) Publish(ctx context.Context, routingKey string, payload []byte) error {
	ch, err := p.conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	return ch.PublishWithContext(ctx, p.exchange, routingKey, false, false, amqp091.Publishing{
		ContentType: "application/json",
		Body:        payload,
	})
}

func (p *RabbitPublisher) Close() error {
	return p.conn.Close()
}
