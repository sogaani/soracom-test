package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sogaani/soracom-test/internal/store"
)

type fakeReader struct{ got store.Query }

func (f *fakeReader) Find(_ context.Context, q store.Query) ([]store.Message, error) {
	f.got = q
	return []store.Message{{Time: time.UnixMilli(1000).UTC(), Meta: store.Meta{IMSI: "1"}, Data: map[string]any{"a": 1.0}}}, nil
}

func (f *fakeReader) Devices(context.Context) ([]string, error) { return []string{"1", "2"}, nil }

func TestMessages(t *testing.T) {
	r := &fakeReader{}
	h := NewHandler(r, slog.New(slog.NewTextHandler(io.Discard, nil)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/messages?from=1000&to=2000&imsi=All&source=sms&limit=5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	want := store.Query{From: time.UnixMilli(1000), To: time.UnixMilli(2000), Source: "sms", Limit: 5}
	if !r.got.From.Equal(want.From) || !r.got.To.Equal(want.To) || r.got.IMSI != "" || r.got.Source != "sms" || r.got.Limit != 5 {
		t.Errorf("query = %+v", r.got)
	}
	var msgs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil || len(msgs) != 1 || msgs[0]["time"] != "1970-01-01T00:00:01Z" {
		t.Errorf("body = %s (%v)", rec.Body, err)
	}

	for _, bad := range []string{"from=x", "limit=0", "from=2000&to=1000"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/messages?"+bad, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", bad, rec.Code)
		}
	}
}
