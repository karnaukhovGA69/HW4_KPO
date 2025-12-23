package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"gozon/orders-service/internal/order"

	"github.com/google/uuid"
)

type Server struct {
	orderSvc *order.Service
	logger   *slog.Logger
	mux      *http.ServeMux
}

func NewServer(orderSvc *order.Service, logger *slog.Logger) *Server {
	s := &Server{
		orderSvc: orderSvc,
		logger:   logger,
		mux:      http.NewServeMux(),
	}

	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /orders", s.createOrder)
	s.mux.HandleFunc("GET /orders", s.listOrders)
	s.mux.HandleFunc("GET /orders/{orderID}", s.getOrder)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	userID, err := s.userIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Amount int64 `json:"amount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	order, err := s.orderSvc.Create(r.Context(), userID, req.Amount)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, order)
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	userID, err := s.userIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	orders, err := s.orderSvc.List(r.Context(), userID)
	if err != nil {
		s.logger.Error("list orders", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	userID, err := s.userIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	orderIDStr := r.PathValue("orderID")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id")
		return
	}

	o, err := s.orderSvc.Get(r.Context(), userID, orderID)
	if err != nil {
		if errors.Is(err, order.ErrOrderNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		s.logger.Error("get order", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, o)
}

func (s *Server) userIDFromRequest(r *http.Request) (uuid.UUID, error) {
	value := r.Header.Get("X-User-ID")
	if value == "" {
		return uuid.UUID{}, errors.New("missing X-User-ID header")
	}
	return uuid.Parse(value)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func WithServer(ctx context.Context, addr string, srv http.Handler) *http.Server {
	server := &http.Server{
		Addr:    addr,
		Handler: srv,
	}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	return server
}
