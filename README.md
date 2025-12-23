# Гоzон: платежи и заказы

Два микросервиса на Go реализуют описанную в задании логику:

- **Payments Service** — создание кошелька пользователя, пополнение и проверка баланса. Сервис потребляет события `orders.created`, использует Transactional Inbox + Outbox и обеспечивает идемпотентные списания.
- **Orders Service** — создание заказа, просмотр списка и статуса. При создании заказа в одной транзакции сохраняется запись и событие в transactional outbox. После получения событий из платежей статус обновляется.

Оба сервиса получают `user_id` из заголовка `X-User-ID` и работают с RabbitMQ (доставка at-least-once) и с отдельными кластерами PostgreSQL.

## Архитектура

```
[Client] -> (HTTP) -> [API Gateway / curl]
        -> Orders Service -> PostgreSQL (orders)
                          -> transactional outbox -> RabbitMQ exchange `orders.events`
        <- Payments Service <- RabbitMQ exchange `orders.events`
                              -> PostgreSQL (payments) с Inbox + Outbox + Ledger
                              -> RabbitMQ exchange `payments.events`
        <- Orders Service <- RabbitMQ exchange `payments.events`
```

Гарантии:

- Заказы создаются с состоянием `pending`. Transactional Outbox гарантирует публикацию события `orders.created`.
- Payments Service вставляет запись в `payment_inbox`, блокирует счёт пользователя и списывает деньги атомарным `UPDATE ... WHERE user_id = $1`. Дедупликация достигается уникальными ключами `payment_inbox` и `payments(order_id)`.
- После списания (или отказа) событие `payments.processed` попадает в Outbox и публикуется в RabbitMQ. Orders Service использует `order_inbox` и обновляет статус на `paid` или `failed`.

## Быстрый старт без контейнеров

1. Запустить PostgreSQL (два экземпляра) и RabbitMQ либо воспользоваться Docker Compose (ниже).
2. Экспортировать переменные окружения (пример):

```bash
export ORDERS_DATABASE_URL="postgres://orders:orders@localhost:5433/orders?sslmode=disable"
export PAYMENTS_DATABASE_URL="postgres://payments:payments@localhost:5434/payments?sslmode=disable"
export ORDERS_RABBIT_URL="amqp://guest:guest@localhost:5672/"
export PAYMENTS_RABBIT_URL="$ORDERS_RABBIT_URL"
```

3. Запустить сервисы:

```bash
GOMODCACHE=$(pwd)/.gomodcache GOCACHE=$(pwd)/.gocache go run ./payments-service/cmd/payments-service
GOMODCACHE=$(pwd)/.gomodcache GOCACHE=$(pwd)/.gocache go run ./orders-service/cmd/orders-service
```

Миграции выполняются автоматически при запуске.

## Docker Compose

```
docker compose up --build
```

Поднимутся:

- RabbitMQ + management UI на `http://localhost:15672`
- PostgreSQL `orders-db` (порт `5433`) и `payments-db` (порт `5434`)
- `payments-service` на `http://localhost:8081`
- `orders-service` на `http://localhost:8080`

## API

Все запросы требуют заголовок `X-User-ID` c UUID пользователя.

Готовая Postman коллекция лежит в `postman/gozon.postman_collection.json` — достаточно поменять переменные `orders_base`, `payments_base` и `user_id`.

### Payments Service (`http://localhost:8081`)

| Метод | Путь                 | Описание                       |
|-------|---------------------|--------------------------------|
| POST  | `/accounts`         | Создать счёт пользователя      |
| POST  | `/accounts/deposit` | Пополнить счёт (`{"amount":}`) |
| GET   | `/accounts/balance` | Получить баланс                |

Пример:

```bash
USER=11111111-1111-1111-1111-111111111111
curl -X POST http://localhost:8081/accounts -H "X-User-ID: $USER"
curl -X POST http://localhost:8081/accounts/deposit -H "X-User-ID: $USER" -d '{"amount":150000}'
curl http://localhost:8081/accounts/balance -H "X-User-ID: $USER"
```

### Orders Service (`http://localhost:8080`)

| Метод | Путь             | Описание                                          |
|-------|-----------------|---------------------------------------------------|
| POST  | `/orders`       | Создать заказ (`{"amount":}` в копейках/центах)   |
| GET   | `/orders`       | Список заказов текущего пользователя              |
| GET   | `/orders/{id}`  | Детали заказа                                     |

Пример:

```bash
ORDER=$(curl -s -X POST http://localhost:8080/orders \
  -H "X-User-ID: $USER" \
  -H "Content-Type: application/json" \
  -d '{"amount": 100000}' | jq -r '.id')

curl http://localhost:8080/orders/$ORDER -H "X-User-ID: $USER"
curl http://localhost:8080/orders -H "X-User-ID: $USER"
```

Создание заказа асинхронно инициирует списание. Статус поменяется на `paid` при успешной обработке платежа или `failed` при недостатке средств / отсутствии счёта. Логику можно наблюдать в логах сервисов и в таблицах `order_inbox`, `payment_inbox`, `payment_outbox`.

## Тестирование

В каждом модуле можно выполнить компиляцию/прогон:

```
cd payments-service && go test ./...
cd orders-service && go test ./...
```

Дополнительно можно использовать Postman/Swagger (не включены в репозиторий) для проверки всех REST-эндпоинтов.
