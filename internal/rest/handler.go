package rest

import (
	"log/slog"

	internalHttp "github.com/kyzrfranz/go-fitter/internal/http"
	restFit "github.com/kyzrfranz/go-fitter/internal/rest/fit"
)

type Handler struct {
	logger *slog.Logger
	Fit    internalHttp.HandlerFunc
}

func NewHandler(logger *slog.Logger) *Handler {

	fitHandler := restFit.NewHandler(logger)

	return &Handler{
		logger: logger,
		Fit:    fitHandler.Handle,
	}
}
