package fit

import (
	"encoding/json"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	activity, err := h.repository.Get(r.Context(), id, h.buildFindOneOptions(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	responseData, err := json.Marshal(activity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(responseData)
}

func (h *Handler) buildFindOneOptions(r *http.Request) options.Lister[options.FindOneOptions] {
	findOpts := options.FindOne()

	projection := bson.M{
		"sessionSummary": 1,
		"meta":           1,
		"sport":          1,
		"sub_sport":      1,
		"start_time":     1,
		"sportMesg":      1,
		"source":         1,
		"laps":           1,
		"custom":         1,
	}

	includeRecords := r.URL.Query().Has("withRecords")
	if includeRecords {
		projection["records"] = 1
	}

	findOpts.SetProjection(projection)

	return findOpts
}
