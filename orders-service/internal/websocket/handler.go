package websocket

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"gozon/orders-service/internal/order"

	"github.com/google/uuid"
	gw "github.com/gorilla/websocket"
)

type Conn = gw.Conn

var upgrader = gw.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Handler struct {
	hub      *Hub
	orderSvc *order.Service
	logger   *slog.Logger
}

func NewHandler(hub *Hub, orderSvc *order.Service) *Handler {
	return &Handler{hub: hub, orderSvc: orderSvc, logger: slog.Default()}
}

func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	orderIDStr := r.PathValue("orderID")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		_ = conn.Close()
		return
	}

	userHeader := r.Header.Get("X-User-ID")
	if userHeader == "" {
		_ = conn.Close()
		return
	}
	userID, err := uuid.Parse(userHeader)
	if err != nil {
		_ = conn.Close()
		return
	}

	o, err := h.orderSvc.Get(r.Context(), userID, orderID)
	if err != nil {
		if errors.Is(err, order.ErrOrderNotFound) {
			_ = conn.Close()
			return
		}
		_ = conn.Close()
		return
	}

	client := &Client{
		hub:     h.hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		orderID: orderIDStr,
	}

	client.hub.register <- client
	go client.writePump()
	go client.readPump()

	upd := OrderUpdate{OrderID: orderIDStr, Status: string(o.Status)}
	if b, err := json.Marshal(upd); err == nil {
		select {
		case client.send <- b:
		case <-time.After(1 * time.Second):
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	defer func() { _ = c.conn.Close() }()
	for msg := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := c.conn.WriteMessage(gw.TextMessage, msg); err != nil {
			return
		}
	}
}
