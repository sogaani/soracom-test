package beam

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sogaani/soracom-test/internal/store"
)

func TestSignDocExample(t *testing.T) {
	// Example from https://users.soracom.io/ja-jp/docs/beam/verify-signature/
	h := http.Header{}
	h.Set(HeaderIMEI, "867612345678901")
	h.Set(HeaderIMSI, "295012345678901")
	h.Set(HeaderTimestamp, "1640962800000")
	if got, want := Sign([]byte("topsecret"), h), "83341a7b3fa0b264e029c338acf83ac07cc416789efe9ace4275a537924aecba"; got != want {
		t.Errorf("Sign = %s, want %s", got, want)
	}
}

func TestVerify(t *testing.T) {
	now := time.UnixMilli(1640962800000)
	signed := func(mod func(http.Header)) http.Header {
		h := http.Header{}
		h.Set(HeaderIMSI, "295012345678901")
		h.Set(HeaderTimestamp, "1640962800000")
		h.Set(HeaderSignatureVersion, "20151001")
		h.Set(HeaderSignature, Sign([]byte("key"), h))
		if mod != nil {
			mod(h)
		}
		return h
	}
	v := &Verifier{Key: []byte("key"), MaxSkew: time.Minute, Now: func() time.Time { return now }}

	tests := []struct {
		name string
		h    http.Header
		want error
	}{
		{"valid", signed(nil), nil},
		{"missing", signed(func(h http.Header) { h.Del(HeaderSignature) }), ErrNoSignature},
		{"tampered imsi", signed(func(h http.Header) { h.Set(HeaderIMSI, "295019999999999") }), ErrBadSignature},
		{"wrong key", signed(func(h http.Header) { h.Set(HeaderSignature, Sign([]byte("other"), h)) }), ErrBadSignature},
		{"no timestamp", signed(func(h http.Header) { h.Del(HeaderTimestamp) }), ErrBadTimestamp},
		{"unknown version", signed(func(h http.Header) { h.Set(HeaderSignatureVersion, "1") }), ErrSignatureVersion},
	}
	for _, tt := range tests {
		if err := v.Verify(tt.h); err != tt.want { //nolint:errorlint // sentinel errors are returned unwrapped
			t.Errorf("%s: Verify = %v, want %v", tt.name, err, tt.want)
		}
	}

	for _, d := range []time.Duration{2 * time.Minute, -2 * time.Minute} {
		v.Now = func() time.Time { return now.Add(d) }
		if err := v.Verify(signed(nil)); err != ErrStaleTimestamp { //nolint:errorlint // sentinel
			t.Errorf("skew %v: Verify = %v", d, err)
		}
	}
	v.MaxSkew = 0
	if err := v.Verify(signed(nil)); err != nil {
		t.Errorf("MaxSkew=0: Verify = %v", err)
	}
}

func TestParse(t *testing.T) {
	now := time.Unix(1700000000, 0)
	tests := []struct {
		name     string
		src      Source
		ct       string
		body     string
		wantData map[string]any
		wantText string
		wantRaw  []byte
	}{
		{
			"http json", SourceHTTP, "application/json", `{"cpuTemp":48.3,"uptime":3600}`,
			map[string]any{"cpuTemp": 48.3, "uptime": int64(3600)},
			"", nil,
		},
		{"http text", SourceHTTP, "text/plain", "hello", nil, "hello", nil},
		{"http binary", SourceHTTP, "application/octet-stream", "\x01\x02", nil, "", []byte{1, 2}},
		{"tcp json payload", SourceTCP, "", `{"payload":"eyJhIjoxfQ=="}`, map[string]any{"a": int64(1)}, "", nil},
		{"udp binary payload", SourceUDP, "", `{"payload":"AQIDBA=="}`, nil, "", []byte{1, 2, 3, 4}},
		{"udp binary parser", SourceUDP, "", `{"cpuTemp":48.3}`, map[string]any{"cpuTemp": 48.3}, "", nil},
		{"sms kv", SourceSMS, "", `{"payload":"temp=48.3,load=0.12"}`, map[string]any{"temp": 48.3, "load": 0.12}, "", nil},
		{"sms text", SourceSMS, "", `{"payload":"hello, world"}`, nil, "hello, world", nil},
		{"sms kv spaces", SourceSMS, "", `{"payload":"temp = 1, load= 2"}`, map[string]any{"temp": 1.0, "load": 2.0}, "", nil},
		{"sms json trailing", SourceSMS, "", `{"payload":"{\"a\":1} x"}`, nil, `{"a":1} x`, nil},
		{
			"ussd", SourceUSSD, "", `{"imsi":"295012345678901","ussdDataCodingScheme":15,"ussdString":"*901001*up=1#","value":"up=1"}`,
			map[string]any{"up": 1.0},
			"", nil,
		},
	}
	for _, tt := range tests {
		h := http.Header{}
		if tt.ct != "" {
			h.Set("Content-Type", tt.ct)
		}
		m, err := Parse(tt.src, h, []byte(tt.body), now)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !reflect.DeepEqual(m.Data, tt.wantData) || m.Text != tt.wantText || !reflect.DeepEqual(m.Raw, tt.wantRaw) {
			t.Errorf("%s: got data=%#v text=%q raw=%v", tt.name, m.Data, m.Text, m.Raw)
		}
		if m.Meta.Source != string(tt.src) || !m.Time.Equal(now) {
			t.Errorf("%s: meta=%+v time=%v", tt.name, m.Meta, m.Time)
		}
	}

	m, err := Parse(SourceUSSD, http.Header{}, []byte(`{"imsi":"295012345678901","value":"x"}`), now)
	if err != nil || m.Meta.IMSI != "295012345678901" {
		t.Errorf("ussd imsi fallback: %+v %v", m, err)
	}
	h := http.Header{}
	h.Set(HeaderTimestamp, "1640962800000")
	if m, _ := Parse(SourceSMS, h, []byte(`{"payload":"x"}`), now); !m.Time.Equal(time.UnixMilli(1640962800000)) {
		t.Errorf("timestamp header not used: %v", m.Time)
	}
	if _, err := Parse(SourceHTTP, http.Header{"Content-Type": {"application/json"}}, []byte(`[1]`), now); err == nil {
		t.Error("expected error for non-object json")
	}
}

type fakeStore struct{ msgs []*store.Message }

func (f *fakeStore) Insert(_ context.Context, m *store.Message) error {
	f.msgs = append(f.msgs, m)
	return nil
}

func TestHandler(t *testing.T) {
	fs := &fakeStore{}
	mux := http.NewServeMux()
	mux.Handle("POST /beam/{source}", &Handler{
		Store:    fs,
		Verifier: &Verifier{Key: []byte("key")},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	post := func(path string, sign bool) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(`{"payload":"temp=1"}`))
		req.Header.Set(HeaderIMSI, "295012345678901")
		req.Header.Set(HeaderTimestamp, "1640962800000")
		if sign {
			req.Header.Set(HeaderSignature, Sign([]byte("key"), req.Header))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := post("/beam/sms", true); got != http.StatusCreated {
		t.Errorf("signed: status %d", got)
	}
	if got := post("/beam/sms", false); got != http.StatusForbidden {
		t.Errorf("unsigned: status %d", got)
	}
	if got := post("/beam/ftp", true); got != http.StatusNotFound {
		t.Errorf("unknown source: status %d", got)
	}
	if len(fs.msgs) != 1 || fs.msgs[0].Meta.IMSI != "295012345678901" || fs.msgs[0].Data["temp"] != 1.0 {
		t.Errorf("stored %+v", fs.msgs)
	}
}
