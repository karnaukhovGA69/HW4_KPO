# Гоzон: платежи и заказы

Два микросервиса на Go реализуют описанную в задании логику:

- **Payments Service** — создание кошелька пользователя, пополнение и проверка баланса. Сервис потребляет события `orders.created`, использует Transactional Inbox + Outbox и обеспечивает идемпотентные списания.
- **Orders Service** — создание заказа, просмотр списка и статуса. При создании заказа в одной транзакции сохраняется запись и событие в transactional outbox. После получения событий из платежей статус обновляется.

Оба сервиса получают `user_id` из заголовка `X-User-ID` и работают с RabbitMQ (доставка at-least-once) и с отдельными кластерами PostgreSQL.

## Архитектура

```
# Гоzон: платежи и заказы

Репозиторий содержит два микросервиса на Go с архитектурой Transactional Outbox + Inbox и обменом событий через RabbitMQ:

- Payments Service — обрабатывает события `orders.created`, ведёт учёт балансов, делает атомарные списания и публикует `payments.processed`.
- Orders Service — создаёт заказы (status=pending), публикует событие `orders.created` и обновляет статус заказа по событиям платежей.

Оба сервиса принимают `X-User-ID` в заголовке (UUID). RabbitMQ используется с at-least-once доставкой; база — PostgreSQL с таблицами inbox/outbox.

## Что нового

- Гарантия exactly-once списания: Payments Service проверяет inbox, затем ищет существующую запись платежа по `order_id`. Если платеж уже завершён (succeeded/failed), процесс не делает повторного списания. Логи: `payment already processed`, `payment created`, `funds deducted`.
- Реальное время для клиентов: Orders Service теперь поддерживает WebSocket (endpoint `/orders/{orderID}/ws`) — клиент подключается сразу после создания заказа и получает обновления статуса (pending → paid/failed).

## Архитектура (упрощённо)

```
[Client] --HTTP--> Orders Service
                  Orders Service -> PostgreSQL (orders + outbox)
                  Orders Service -> publishes orders.created -> RabbitMQ
                  Payments Service <- RabbitMQ orders.created
                  Payments Service -> PostgreSQL (payments + outbox + ledger)
                  Payments Service -> publishes payments.processed -> RabbitMQ
                  Orders Service <- RabbitMQ payments.processed (inbox) -> updates order status

Client may also open WebSocket to Orders Service to receive Order status updates in realtime.
```

## Быстрый старт

1) Запустите инфраструктуру (Postgres, RabbitMQ). Для простоты можно использовать docker compose:

```bash
docker compose up --build
```

2) Подготовьте переменные окружения (пример):

```bash
export ORDERS_DATABASE_URL="postgres://orders:orders@localhost:5433/orders?sslmode=disable"
export PAYMENTS_DATABASE_URL="postgres://payments:payments@localhost:5434/payments?sslmode=disable"
export ORDERS_RABBIT_URL="amqp://guest:guest@localhost:5672/"
export PAYMENTS_RABBIT_URL="$ORDERS_RABBIT_URL"
```

3) Установите зависимости и запустите сервисы локально (в корне репозитория):

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

## Проверка exactly-once

1. Создайте заказ через Orders API.
2. Убедитесь, что Payments Service получил событие и создал запись платежа (лог `payment created`) и, при успехе, `funds deducted`.
3. Повторно отправьте то же событие `orders.created` (симуляция повторной доставки) — Payments Service должен логировать `payment already processed` и не списывать деньги повторно.

## Тестирование и сборка

```bash
cd payments-service && go test ./...
cd ../orders-service && go test ./...

# сборка
go build ./...
```

## Примечания для разработчика

- В `orders-service/go.mod` добавлена зависимость `github.com/gorilla/websocket`.
- Логи сервисов содержат сообщения для отладки процесса списания и вебсокет-событий.

Если хотите, могу добавить пример клиентского скрипта для WebSocket и/unit тесты для payment processor.
