package main

import (
	"log"

	"gozon/payments-service/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatalf("payments service failed: %v", err)
	}
}
