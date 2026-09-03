package dropbox

import (
	"testing"
)

func TestConnect(t *testing.T) {
	cli := New("2dmmo9ozi5sayor", "pea8f0yv7o8vux3", "-_ej2PULnyoAAAAAAAAAAYtzWWa_51hZTKA-dByTUK6CaNdq_5IUskGyWIF3i3D3", "zip")
	results, err := cli.Retrieve("/Apps/RunGap/export")
	if err != nil {
		t.Errorf("Expected no error for empty accountId and token, got nil")
	}

	for _, res := range results {
		t.Logf("Found file: %s", res)
		data, err := cli.Read(res)
		if err != nil {
			t.Errorf("Error retrieving file data for %s: %v", res, err)
			continue
		}
		if data == nil {
			t.Errorf("Expected file data, got nil")
		} else {
			t.Logf("Successfully retrieved file data for: %s", res)
		}
	}
}
