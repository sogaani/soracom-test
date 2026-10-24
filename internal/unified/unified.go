// Package unified sends data to the SORACOM Unified Endpoint over the IP
// entry points available to SORACOM Air for Cellular (HTTP, TCP, UDP).
//
// Entry points: https://users.soracom.io/ja-jp/docs/unified-endpoint/entry-points/
package unified

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultHost is the Unified Endpoint hostname. It only resolves from inside
	// the SORACOM network (Air SIM or Arc). "unified.soracom.io" is an alias.
	DefaultHost = "uni.soracom.io"

	// DefaultHTTPPort is the HTTP entry point port. 8888 is also accepted.
	DefaultHTTPPort = 80

	// DefaultSocketPort is the TCP and UDP entry point port.
	DefaultSocketPort = 23080
)

// Response is what the Unified Endpoint returned.
type Response struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

// UnifiedBody is the body returned when the group's response format is
// "Unified" (or "Auto" with two or more destinations).
// https://users.soracom.io/ja-jp/docs/unified-endpoint/settings/
type UnifiedBody struct {
	Result string                   `json:"result"` // "ok" or "ng"
	Detail map[string]ServiceResult `json:"detail"` // keyed by SoracomBeam, SoracomFunk, SoracomFunnel, SoracomHarvest
}

// ServiceResult is the result of forwarding to one destination service.
type ServiceResult struct {
	StatusCode  int               `json:"statusCode"`
	ContentType string            `json:"contentType,omitempty"`
	Body        string            `json:"body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Encoding    string            `json:"encoding,omitempty"` // "base64" for Funk
}

// DecodedBody returns the body, decoding it if it is base64 encoded.
func (s ServiceResult) DecodedBody() string {
	if s.Encoding == "base64" {
		if b, err := base64.StdEncoding.DecodeString(s.Body); err == nil {
			return string(b)
		}
	}
	return s.Body
}

// Unified parses the body as the Unified response format. It returns false
// if the body is not in that format.
func (r *Response) Unified() (*UnifiedBody, bool) {
	var u UnifiedBody
	if err := json.Unmarshal(r.Body, &u); err != nil || u.Result == "" || u.Detail == nil {
		return nil, false
	}
	return &u, true
}

// String formats the response for display.
func (r *Response) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "status: %d\n", r.StatusCode)
	if r.ContentType != "" {
		fmt.Fprintf(&sb, "content-type: %s\n", r.ContentType)
	}
	if u, ok := r.Unified(); ok {
		fmt.Fprintf(&sb, "result: %s\n", u.Result)
		for name, d := range u.Detail {
			fmt.Fprintf(&sb, "  %s: status=%d", name, d.StatusCode)
			if d.Body != "" {
				fmt.Fprintf(&sb, " body=%q", d.DecodedBody())
			}
			sb.WriteString("\n")
		}
	} else if len(r.Body) > 0 {
		fmt.Fprintf(&sb, "body: %s\n", r.Body)
	}
	return sb.String()
}

// parseSocketResponse parses a TCP/UDP response, which is "{status} {body}"
// (e.g. "201", "200 Hi", "200 {\"result\":\"ok\",...}"). When a Custom
// response omits the status code, the whole payload is returned as the body
// with StatusCode 0.
func parseSocketResponse(b []byte) *Response {
	s := string(b)
	code, rest, _ := strings.Cut(s, " ")
	n, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil || n < 100 || n > 599 {
		return &Response{Body: b}
	}
	return &Response{StatusCode: n, Body: []byte(rest)}
}
