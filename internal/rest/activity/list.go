package fit

import (
	"encoding/json"
	"net/http"

	"github.com/kyzrfranz/go-fitter/internal/rest"
)

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := rest.BuildActivityFilter(q)

	activities, _ := h.repository.List(r.Context(), filter, rest.BuildFindOptions(q, false, false))
	w.Header().Set("Content-Type", "application/json")
	responseData, err := json.Marshal(activities)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(responseData)
}
