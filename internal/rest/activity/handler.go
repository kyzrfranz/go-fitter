package fit

import (
	"io"
	"log/slog"
	"net/http"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
)

// RawReader is the subset of the syncer Retriever the chart endpoint needs:
// open the source file for an activity by its stored Source.Path.
type RawReader interface {
	Read(path string) (io.ReadCloser, error)
}

type Handler struct {
	logger     *slog.Logger
	repository db.DatabaseClient[v1.Activity]
	retriever  RawReader
}

func NewHandler(logger *slog.Logger, repository db.DatabaseClient[v1.Activity], retriever RawReader) *Handler {
	return &Handler{
		logger:     logger,
		repository: repository,
		retriever:  retriever,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	h.list(w, r)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	h.get(w, r)
}
