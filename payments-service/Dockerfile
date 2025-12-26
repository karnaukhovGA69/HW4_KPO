FROM golang:1.25 AS builder
WORKDIR /workspace

COPY go.work go.work
COPY orders-service orders-service
COPY payments-service payments-service
COPY pkg pkg

RUN cd payments-service && go mod download && go mod verify
RUN cd pkg && go mod download && go mod verify

RUN cd payments-service && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /payments-service ./cmd/payments-service

FROM gcr.io/distroless/base-debian12
COPY --from=builder /payments-service /payments-service
EXPOSE 8081
ENTRYPOINT ["/payments-service"]
