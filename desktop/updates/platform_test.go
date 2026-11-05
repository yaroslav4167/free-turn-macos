package updates

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMacUpdateSelectsOnlyCurrentArchitecture(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		m := NewForPlatform("v0.1.0", arch, "example/mac", "darwin")
		m.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			assets := []asset{}
			for _, a := range []string{"arm64", "amd64"} {
				name := "FreeTurn-macOS-" + a + "-v0.2.0.zip"
				assets = append(assets, asset{Name: name, URL: "https://github.com/example/mac/releases/download/v0.2.0/" + name, Size: 1024})
			}
			assets = append(assets, asset{Name: "checksums.txt", URL: "https://github.com/example/mac/releases/download/v0.2.0/checksums.txt"})
			data, _ := json.Marshal(release{Tag: "v0.2.0", Assets: assets})
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
		})
		if err := m.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := m.State(); got.AssetName != "FreeTurn-macOS-"+arch+"-v0.2.0.zip" || !got.Available {
			t.Fatalf("wrong update: %+v", got)
		}
	}
}
