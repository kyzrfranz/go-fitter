package fit

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
	"github.com/kyzrfranz/go-fitter/pkg/converters"
	cJson "github.com/kyzrfranz/go-fitter/pkg/converters/fit/json"
	"github.com/muktihari/fit/decoder"
)

const (
	fitFileKey  = "fit"
	metaFileKey = "meta"
)

func (h *Handler) postHandler(w http.ResponseWriter, r *http.Request) {
	fitFile, err := getFile(r, fitFileKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	metaFile, err := getFile(r, metaFileKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	var decoderOptions []decoder.Option
	decoderOptions = append(decoderOptions, decoder.WithIgnoreChecksum())

	var jsonOpts []cJson.Option
	// get params "records"
	records := r.URL.Query().Has("records")
	if !records {
		jsonOpts = append(jsonOpts, cJson.WithNoRecords())
	}

	activities, err := converters.FitToActivity(fitFile, decoderOptions, jsonOpts...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	var meta *v1.ActivityMeta
	if metaFile != nil {
		var m v1.ActivityMeta
		metaFileBytes, _ := io.ReadAll(metaFile)
		json.Unmarshal(metaFileBytes, &m)
		meta = &m
	}

	// A multisport file yields one activity per leg; persist each.
	for _, activity := range activities {
		if meta != nil {
			activity.Meta = *meta
		}

		// Records are returned to the caller but never persisted.
		persisted := *activity
		persisted.Records = nil
		created, err := h.repository.Create(r.Context(), &persisted)
		if err != nil {
			db.ErrorToHttpError(w, err)
			return
		}
		h.logger.Log(r.Context(), slog.LevelInfo, "created activity", "created", created)
	}

	responseData, err := json.Marshal(activities)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseData) // Write the JSON data
}

func getFile(r *http.Request, name string) (io.Reader, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, err
	}

	file, _, err := r.FormFile(name)
	if err != nil {
		return nil, err
	}
	return file, nil
}
