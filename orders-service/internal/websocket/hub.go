package websocket

import (
	"context"
	"encoding/json"
)

type OrderUpdate struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type Client struct {
	hub     *Hub
	conn    *Conn
	send    chan []byte
	orderID string
}

type Hub struct {
	register   chan *Client
	unregister chan *Client
	broadcast  chan OrderUpdate
	clients    map[string]map[*Client]bool
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan OrderUpdate),
		clients:    make(map[string]map[*Client]bool),
	}
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case c := <-h.register:
			set, ok := h.clients[c.orderID]
			if !ok {
				set = make(map[*Client]bool)
				h.clients[c.orderID] = set
			}
			h.clients[c.orderID][c] = true
		case c := <-h.unregister:
			if set, ok := h.clients[c.orderID]; ok {
				if _, exists := set[c]; exists {
					delete(set, c)
					close(c.send)
				}
				if len(set) == 0 {
					delete(h.clients, c.orderID)
				}
			}
		case upd := <-h.broadcast:
			msg, _ := json.Marshal(upd)
			if set, ok := h.clients[upd.OrderID]; ok {
				for c := range set {
					select {
					case c.send <- msg:
					default:
						delete(set, c)
						close(c.send)
					}
				}
			}
		case <-ctx.Done():
			for _, set := range h.clients {
				for c := range set {
					close(c.send)
				}
			}
			return
		}
	}
}

func (h *Hub) Broadcast(u OrderUpdate) {
	go func() { h.broadcast <- u }()
}

func (h *Hub) BroadcastOrderUpdate(orderID string, status string) {
	h.Broadcast(OrderUpdate{OrderID: orderID, Status: status})
}
