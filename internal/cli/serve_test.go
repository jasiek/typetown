package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jasiek/typetown"
	"github.com/jasiek/typetown/internal/testindex"
)

func openFixture(t *testing.T) *typetown.Index {
	t.Helper()
	ix, err := typetown.Open(testindex.Build(t))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestServeMux(t *testing.T) {
	ix := openFixture(t)
	var logged bytes.Buffer
	h := serveMux(ix, "/places", []typetown.HandlerOption{typetown.WithHome("GB")}, false, &logged)

	t.Run("serves lookups at the chosen path", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/places?q=lond", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body struct {
			Results []struct{ Name, CC string } `json:"results"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Results) == 0 || body.Results[0].Name != "London" {
			t.Errorf("got %+v", body.Results)
		}
	})

	t.Run("healthz reports what the index holds", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var stats typetown.Stats
		if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
			t.Fatal(err)
		}
		if stats.Records != len(testindex.Default) {
			t.Errorf("records = %d, want %d", stats.Records, len(testindex.Default))
		}
	})

	t.Run("logs one line per request", func(t *testing.T) {
		logged.Reset()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/places?q=lond&limit=2", nil))
		line := logged.String()
		for _, want := range []string{"GET", "/places?q=lond&limit=2", "200"} {
			if !strings.Contains(line, want) {
				t.Errorf("log line %q does not contain %q", line, want)
			}
		}
		if strings.Count(strings.TrimSpace(line), "\n") != 0 {
			t.Errorf("expected exactly one line, got %q", line)
		}
	})

	// A failing request must be logged with the status it actually returned, not
	// the 200 the recorder starts at.
	t.Run("logs the real status", func(t *testing.T) {
		logged.Reset()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/places", nil))
		if !strings.Contains(logged.String(), "400") {
			t.Errorf("log line %q does not mention 400", logged.String())
		}
	})
}

func TestServeMuxPathAndQuiet(t *testing.T) {
	ix := openFixture(t)

	// A path given without a leading slash still mounts where the user meant.
	var quietLog bytes.Buffer
	h := serveMux(ix, "api/places", nil, true, &quietLog)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/places?q=wien", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if quietLog.Len() != 0 {
		t.Errorf("quiet mode still logged %q", quietLog.String())
	}
}

func TestSplitOrigins(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"*", []string{"*"}},
		{"https://a.example", []string{"https://a.example"}},
		{"https://a.example, https://b.example", []string{"https://a.example", "https://b.example"}},
		{" , https://a.example , ", []string{"https://a.example"}},
		{",", nil},
	}
	for _, tt := range tests {
		got := splitOrigins(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("splitOrigins(%q) = %q, want %q", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitOrigins(%q) = %q, want %q", tt.in, got, tt.want)
				break
			}
		}
	}
}
