package handler

import (
	"log/slog"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
	restActivity "github.com/kyzrfranz/go-fitter/internal/rest/activity"
	restHealth "github.com/kyzrfranz/go-fitter/internal/rest/health"
	llm_context "github.com/kyzrfranz/go-fitter/internal/rest/llm-context"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Handler struct {
	logger     *slog.Logger
	Health     *restHealth.Handler
	Activity   *restActivity.Handler
	LLMContext *llm_context.Handler
}

func NewHandler(logger *slog.Logger, mongoCli *mongo.Client, dbName string, retriever restActivity.RawReader) *Handler {

	activityRepo := db.NewItemsRepository[v1.Activity](mongoCli, dbName, "activities")
	healthRepo := db.NewItemsRepository[db.HealthMetric](mongoCli, dbName, "health")
	healthHandler := restHealth.NewHandler(logger, healthRepo)
	activityHandler := restActivity.NewHandler(logger, activityRepo, retriever)
	llmContextHandler := llm_context.NewHandler(logger, activityRepo, healthRepo)

	return &Handler{
		logger:     logger,
		Health:     healthHandler,
		Activity:   activityHandler,
		LLMContext: llmContextHandler,
	}
}
