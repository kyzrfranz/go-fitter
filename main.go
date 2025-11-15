package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kyzrfranz/go-fitter/pkg/converters"
	cJson "github.com/kyzrfranz/go-fitter/pkg/converters/json"
	"github.com/muktihari/fit/decoder"
)

func main() {
	test()
}

func test() {
	var printOnlyValidValue bool
	flag.BoolVar(&printOnlyValidValue, "valid", false, "Print only valid value")

	var printDegrees bool
	flag.BoolVar(&printDegrees, "deg", false, "Print GPS position (Lat & Long) in degrees instead of semicircles")

	var noExpand bool
	flag.BoolVar(&noExpand, "no-expand", false, "[Decode Option] Do not expand components")

	var noChecksum bool
	flag.BoolVar(&noChecksum, "no-checksum", false, "[Decode Option] should not do crc checksum")

	var noRecords bool
	flag.BoolVar(&noRecords, "no-records", false, "Exclude the high-resolution 'records' array from the JSON output")

	flag.Parse()

	var decoderOptions []decoder.Option
	if noExpand {
		decoderOptions = append(decoderOptions, decoder.WithNoComponentExpansion())
	}
	if noChecksum {
		decoderOptions = append(decoderOptions, decoder.WithIgnoreChecksum())
	}

	var jsonOpts []cJson.Option
	// Add existing options (like -deg, --valid)
	if printDegrees {
		jsonOpts = append(jsonOpts, cJson.WithPrintGPSPositionInDegrees())
	}
	if printOnlyValidValue {
		jsonOpts = append(jsonOpts, cJson.WithPrintOnlyValidValue())
	}

	// Add the new option
	if noRecords {
		jsonOpts = append(jsonOpts, cJson.WithNoRecords())
	}

	paths := flag.Args()

	if len(paths) == 0 {
		panic("missing file argument, e.g.: fitconv Activity.fit\n")
	}

	for _, path := range paths {
		ext := filepath.Ext(path)
		switch ext {
		case ".fit":
			msg, err := converters.FitToJson(path, decoderOptions, jsonOpts...)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not convert %q to json: %v\n", path, err)
			}

			fmt.Fprintf(os.Stdout, "%s\n", msg)
		default:
			fmt.Fprintf(os.Stderr, "unrecognized format: %s\n", ext)
		}
	}
}
