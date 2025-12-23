package main

import (
	"log"

	"gozon/orders-service/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatalf("orders service failed: %v", err)
	}
}
