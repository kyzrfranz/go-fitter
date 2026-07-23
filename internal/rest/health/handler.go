package fit

import (
	"log/slog"
	"net/http"

	"github.com/kyzrfranz/go-fitter/internal/db"
)

type Handler struct {
	logger *slog.Logger
	repo   db.DatabaseClient[db.HealthMetric]
}

func NewHandler(logger *slog.Logger, repo db.DatabaseClient[db.HealthMetric]) *Handler {
	return &Handler{
		logger: logger,
		repo:   repo,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	h.list(w, r)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	h.get(w, r)
}
