package beam

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sogaani/soracom-test/internal/store"
)

// Source is the protocol the device used to reach the Unified Endpoint.
// Beam does not tell the destination which entry point a request came
// through, so each Beam entry point is configured to forward to its own path
// (/beam/<source>).
type Source string

const (
	SourceHTTP Source = "http"
	SourceTCP  Source = "tcp"
	SourceUDP  Source = "udp"
	SourceSMS  Source = "sms"
	SourceUSSD Source = "ussd"
)

// ParseSource validates a source name taken from the request path.
func ParseSource(s string) (Source, bool) {
	switch src := Source(s); src {
	case SourceHTTP, SourceTCP, SourceUDP, SourceSMS, SourceUSSD:
		return src, true
	default:
		return "", false
	}
}

// Parse converts a Beam request body into a message. Body formats:
//
//   - http: the device's body as sent, interpreted by Content-Type.
//   - tcp, udp: {"payload":"<base64>"}, or the Binary Parser / Orbit output
//     JSON when one is configured.
//     https://users.soracom.io/ja-jp/docs/beam/tcp-http/
//   - sms: {"payload":"<text>"}
//     https://users.soracom.io/ja-jp/docs/beam/sms-http/
//   - ussd: {"imsi":"...","ussdDataCodingScheme":15,"ussdString":"*901001*123#","value":"123"}
//     https://users.soracom.io/ja-jp/docs/beam/ussd-http/
func Parse(src Source, h http.Header, body []byte, now time.Time) (*store.Message, error) {
	m := &store.Message{
		Time: now,
		Meta: store.Meta{
			IMSI:   h.Get(HeaderIMSI),
			IMEI:   h.Get(HeaderIMEI),
			SIMID:  h.Get(HeaderSIMID),
			MSISDN: h.Get(HeaderMSISDN),
			Source: string(src),
		},
		ContentType: h.Get("Content-Type"),
	}
	if ts, err := Timestamp(h); err == nil {
		m.Time = ts
	}

	switch src {
	case SourceHTTP:
		mt, _, err := mime.ParseMediaType(m.ContentType)
		if err != nil {
			mt = "" // missing or malformed: store as binary
		}
		switch {
		case mt == "application/json" || strings.HasSuffix(mt, "+json"):
			obj, err := jsonObject(body)
			if err != nil {
				return nil, fmt.Errorf("invalid json body: %w", err)
			}
			m.Data = obj
		case strings.HasPrefix(mt, "text/"):
			setText(m, string(body))
		default:
			setBytes(m, body)
		}

	case SourceTCP, SourceUDP:
		var env struct {
			Payload *string `json:"payload"`
		}
		obj, err := jsonObject(body)
		if err != nil {
			return nil, fmt.Errorf("invalid beam body: %w", err)
		}
		if json.Unmarshal(body, &env) == nil && env.Payload != nil && len(obj) == 1 {
			raw, err := base64.StdEncoding.DecodeString(*env.Payload)
			if err != nil {
				return nil, fmt.Errorf("invalid base64 payload: %w", err)
			}
			setBytes(m, raw)
		} else {
			// Binary Parser / Orbit output.
			m.Data = obj
		}

	case SourceSMS:
		var env struct {
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("invalid sms body: %w", err)
		}
		setText(m, env.Payload)

	case SourceUSSD:
		var env struct {
			IMSI  string `json:"imsi"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("invalid ussd body: %w", err)
		}
		if m.Meta.IMSI == "" {
			m.Meta.IMSI = env.IMSI
		}
		setText(m, env.Value)
	}
	return m, nil
}

// setBytes stores a binary payload, but keeps it as JSON or text when the
// device sent a textual payload over TCP/UDP (as the demo client does).
func setBytes(m *store.Message, b []byte) {
	if obj, err := jsonObject(b); err == nil {
		m.Data = obj
		return
	}
	if utf8.Valid(b) && isPrintable(string(b)) {
		setText(m, string(b))
		return
	}
	m.Raw = b
}

// setText stores a text payload. JSON objects and "k=v,k=v" text with
// numeric values (the demo client's SMS/USSD format) become Data so they can
// be graphed.
func setText(m *store.Message, s string) {
	if obj, err := jsonObject([]byte(s)); err == nil {
		m.Data = obj
		return
	}
	if kv, ok := parseKeyValues(s); ok {
		m.Data = kv
		return
	}
	m.Text = s
}

func jsonObject(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("not a json object")
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing data after json object")
	}
	normalizeNumbers(obj) // converts in place
	return obj, nil
}

// normalizeNumbers converts json.Number to int64 when integral, else float64,
// so MongoDB stores numbers rather than strings.
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64() //nolint:errcheck // json.Number is always a valid float literal
		return f
	case map[string]any:
		for k, e := range t {
			t[k] = normalizeNumbers(e)
		}
	case []any:
		for i, e := range t {
			t[i] = normalizeNumbers(e)
		}
	}
	return v
}

func parseKeyValues(s string) (map[string]any, bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	out := make(map[string]any, len(parts))
	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return nil, false
		}
		out[strings.TrimSpace(k)] = f
	}
	return out, true
}

func isPrintable(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}
