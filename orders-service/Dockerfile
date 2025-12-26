FROM golang:1.25 AS builder
WORKDIR /workspace

COPY go.work go.work
COPY orders-service orders-service
COPY payments-service payments-service
COPY pkg pkg

RUN cd orders-service && go mod download && go mod verify
RUN cd pkg && go mod download && go mod verify

RUN cd orders-service && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /orders-service ./cmd/orders-service

FROM gcr.io/distroless/base-debian12
COPY --from=builder /orders-service /orders-service
EXPOSE 8080
ENTRYPOINT ["/orders-service"]
