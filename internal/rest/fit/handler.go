package fit

import (
	"log/slog"
	"net/http"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
)

type Handler struct {
	logger     *slog.Logger
	repository db.DatabaseClient[v1.Activity]
}

func NewHandler(logger *slog.Logger, repository db.DatabaseClient[v1.Activity]) *Handler {
	return &Handler{
		logger:     logger,
		repository: repository,
	}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		h.postHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
