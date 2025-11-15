package converters

import (
	"fmt"
	"os"

	cJson "github.com/kyzrfranz/go-fitter/pkg/converters/json"
	"github.com/muktihari/fit/decoder"
)

func FitToJson(path string, decoderOptions []decoder.Option, opts ...cJson.Option) (string, error) {
	ff, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %s: %w", path, err)
	}
	defer ff.Close()

	// We don't need a bufio.Writer, json.Marshal writes it all at once at the end
	conv := cJson.NewFITToJSONConv(opts...) // Use the new converter

	options := []decoder.Option{
		decoder.WithMesgDefListener(conv),
		decoder.WithMesgListener(conv),
		decoder.WithBroadcastOnly(),
		decoder.WithBroadcastMesgCopy(),
	}
	options = append(options, decoderOptions...)
	dec := decoder.New(ff, options...)

	for dec.Next() {
		_, err = dec.Decode()
		if err != nil {
			break
		}
	}

	conv.Wait() // This is where the JSON is marshaled and written

	if err != nil {
		return "", fmt.Errorf("decode failed: %w", err)
	}

	if err := conv.Err(); err != nil {
		return "", fmt.Errorf("convert done with error: %v", err)
	}

	result := conv.Result()

	return result, nil
}
