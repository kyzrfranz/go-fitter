package dropbox

import (
	"archive/zip"
	"bytes"
	"io"
	"path/filepath"
	"strings"
)

func retrieveFitFromZip(zipStream io.ReadCloser) (io.Reader, error) {
	defer zipStream.Close()

	bodyBytes, err := io.ReadAll(zipStream)
	if err != nil {
		return nil, err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
	if err != nil {
		return nil, err
	}

	for _, f := range zipReader.File {
		if strings.ToLower(filepath.Ext(f.Name)) != ".fit" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(content), nil
	}

	return nil, nil
}
