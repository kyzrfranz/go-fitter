package fit

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/kyzrfranz/go-fitter/pkg/converters"
	cJson "github.com/kyzrfranz/go-fitter/pkg/converters/json"
	"github.com/muktihari/fit/decoder"
)

func (h *Handler) postHandler(w http.ResponseWriter, r *http.Request) {
	file, err := getFile(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	var decoderOptions []decoder.Option
	decoderOptions = append(decoderOptions, decoder.WithIgnoreChecksum())

	var jsonOpts []cJson.Option
	jsonOpts = append(jsonOpts, cJson.WithNoRecords())

	msg, err := converters.FitToJson(file, decoderOptions, jsonOpts...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	h.logger.Log(r.Context(), slog.LevelDebug, "data", msg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(msg)) // Write the JSON data
}

func getFile(r *http.Request) (io.Reader, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, err
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, err
	}
	return file, nil
}
