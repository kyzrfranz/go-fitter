package llm_context

import (
	"log/slog"
	"net/http"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
)

type Handler struct {
	logger     *slog.Logger
	repository db.DatabaseClient[v1.Activity]
	healthRepo db.DatabaseClient[db.HealthMetric]
}

func NewHandler(logger *slog.Logger, repository db.DatabaseClient[v1.Activity], healthRepo db.DatabaseClient[db.HealthMetric]) *Handler {
	return &Handler{
		logger:     logger,
		repository: repository,
		healthRepo: healthRepo,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	h.list(w, r)
}
