
# **Payments Service**
 Cоздание кошелька пользователя, пополнение и проверка баланса. Сервис потребляет события `orders.created`, использует Transactional Inbox + Outbox и обеспечивает идемпотентные списания.
# **Orders Service** 
 Cоздание заказа, просмотр списка и статуса. При создании заказа в одной транзакции сохраняется запись и событие в transactional outbox. После получения событий из платежей статус обновляется.

Оба сервиса получают `user_id` из заголовка `X-User-ID` и работают с RabbitMQ (доставка at-least-once) и с отдельными кластерами PostgreSQL.

## Архитектура

```
[Client] --HTTP--> Orders Service
                  Orders Service -> PostgreSQL (orders + outbox)
                  Orders Service -> publishes orders.created -> RabbitMQ
                  Payments Service <- RabbitMQ orders.created
                  Payments Service -> PostgreSQL (payments + outbox + ledger)
                  Payments Service -> publishes payments.processed -> RabbitMQ
                  Orders Service <- RabbitMQ payments.processed (inbox) -> updates order status

Клиент также может открыть сервис WebSocket to Orders, чтобы получать обновления статуса заказа в режиме реального времени.
```

2) Подготовьте переменные окружения (пример):

```bash
export ORDERS_DATABASE_URL="postgres://orders:orders@localhost:5433/orders?sslmode=disable"
export PAYMENTS_DATABASE_URL="postgres://payments:payments@localhost:5434/payments?sslmode=disable"
export ORDERS_RABBIT_URL="amqp://guest:guest@localhost:5672/"
export PAYMENTS_RABBIT_URL="$ORDERS_RABBIT_URL"
```

3) Установите зависимости:

```bash
cd orders-service && go mod tidy
cd ../payments-service && go mod tidy

# в отдельных терминалах
go run ./payments-service/cmd/payments-service
go run ./orders-service/cmd/orders-service
```

Миграции выполняются автоматически при старте.

## WebSocket (Orders Service)

- Endpoint: `GET /orders/{orderID}/ws`
- Заголовок `X-User-ID` обязателен и должен совпадать с владельцем заказа.
- Формат сообщений от сервера: JSON `{ "order_id": "...", "status": "pending|paid|failed" }`.

Пример подключения (wscat):

```bash
#wscat -c "ws://localhost:8080/orders/<ORDER_ID>/ws" -H "X-User-ID: <USER_UUID>"
```

При первом подключении клиент сразу получит текущий статус заказа, затем будет получать обновления в реальном времени.

## API

Все запросы требуют заголовок `X-User-ID`.

### Payments Service (по умолчанию `http://localhost:8081`)

POST /accounts — создать счёт
POST /accounts/deposit — пополнить счёт {"amount": <int>}
GET  /accounts/balance — получить баланс

### Orders Service (по умолчанию `http://localhost:8080`)

POST /orders — создать заказ {"amount": <int>} (возвращает order.id)
GET  /orders — список заказов пользователя
GET  /orders/{id} — детали заказа

После создания заказа Payments Service обработает списание асинхронно и отправит `payments.processed`; Orders Service применит результат с помощью inbox механизмов.
