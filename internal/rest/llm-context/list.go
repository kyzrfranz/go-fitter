package llm_context

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kyzrfranz/go-fitter/internal/rest"
	fit "github.com/kyzrfranz/go-fitter/internal/rest/health"
	llm_context "github.com/kyzrfranz/go-fitter/pkg/converters/activity/llm-context"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := rest.BuildActivityFilter(q)

	to := time.Now()
	from := to.AddDate(0, 0, -7)
	fromQuery := r.URL.Query().Get("from")
	toQuery := r.URL.Query().Get("to")

	if fromQuery == "" {
		fromQuery = from.Format("2006-01-02")
	}

	if toQuery == "" {
		toQuery = to.Format("2006-01-02")
	}

	activities, _ := h.repository.List(r.Context(), filter, rest.BuildFindOptions(q, false, true))
	hFilter := rest.BuildHealthFilter(q)

	var TargetHealthMetrics = []string{
		"resting_heart_rate",
		"heart_rate_variability",
		"sleep_analysis",
		"weight_body_mass",
		"active_energy",
	}
	hFilter["name"] = bson.M{"$in": TargetHealthMetrics}

	healthData, _ := h.healthRepo.List(r.Context(), hFilter, rest.BuildHealthFindOptions(q))

	llmContext := llm_context.Convert(activities, fit.AggregateAndClean(healthData))
	llmContext.ContextWindow = fmt.Sprintf("%s to %s", fromQuery, toQuery)

	w.Header().Set("Content-Type", "application/json")
	responseData, err := json.Marshal(llmContext)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(responseData)
}
