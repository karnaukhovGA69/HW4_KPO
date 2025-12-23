package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"gozon/payments-service/internal/account"

	"github.com/google/uuid"
)

type Server struct {
	accounts *account.Service
	logger   *slog.Logger
	mux      *http.ServeMux
}

func NewServer(accounts *account.Service, logger *slog.Logger) *Server {
	s := &Server{
		accounts: accounts,
		logger:   logger,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /accounts", s.createAccount)
	s.mux.HandleFunc("POST /accounts/deposit", s.deposit)
	s.mux.HandleFunc("GET /accounts/balance", s.balance)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := s.userID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.accounts.Create(r.Context(), userID); err != nil {
		if errors.Is(err, account.ErrAccountExists) {
			writeError(w, http.StatusConflict, "account already exists")
			return
		}
		s.logger.Error("create account", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (s *Server) deposit(w http.ResponseWriter, r *http.Request) {
	userID, err := s.userID(r)
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
	balance, err := s.accounts.Deposit(r.Context(), userID, req.Amount)
	if err != nil {
		switch {
		case errors.Is(err, account.ErrAccountNotFound):
			writeError(w, http.StatusNotFound, "account not found")
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": balance})
}

func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	userID, err := s.userID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	balance, err := s.accounts.GetBalance(r.Context(), userID)
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		s.logger.Error("get balance", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": balance})
}

func (s *Server) userID(r *http.Request) (uuid.UUID, error) {
	value := r.Header.Get("X-User-ID")
	if value == "" {
		return uuid.Nil, errors.New("missing X-User-ID header")
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
