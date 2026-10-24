// Package api serves stored messages as JSON for Grafana (Infinity data
// source). It is read-only and must stay on the cluster-internal listener:
// it has no authentication and is never routed through the public ingress.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/sogaani/soracom-test/internal/store"
)

const (
	defaultRange = 24 * time.Hour
	defaultLimit = 10000
	maxLimit     = 100000
)

// Reader reads stored messages.
type Reader interface {
	Find(ctx context.Context, q store.Query) ([]store.Message, error)
	Devices(ctx context.Context) ([]string, error)
}

// NewHandler returns the query API:
//
//	GET /api/v1/messages?from=<unix ms>&to=<unix ms>&imsi=<imsi>&source=<source>&limit=<n>
//	GET /api/v1/devices
//
// from/to default to the last 24 hours; Grafana passes ${__from} and ${__to}.
func NewHandler(r Reader, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/messages", func(w http.ResponseWriter, req *http.Request) {
		q, err := parseQuery(req, time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		msgs, err := r.Find(req.Context(), q)
		if err != nil {
			log.Error("find messages", "err", err)
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, log, msgs)
	})
	mux.HandleFunc("GET /api/v1/devices", func(w http.ResponseWriter, req *http.Request) {
		imsis, err := r.Devices(req.Context())
		if err != nil {
			log.Error("list devices", "err", err)
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, log, imsis)
	})
	return mux
}

type badRequest string

func (e badRequest) Error() string { return string(e) }

func parseQuery(req *http.Request, now time.Time) (store.Query, error) {
	v := req.URL.Query()
	q := store.Query{
		From:   now.Add(-defaultRange),
		To:     now,
		IMSI:   v.Get("imsi"),
		Source: v.Get("source"),
		Limit:  defaultLimit,
	}
	// Grafana sends "All" for a multi-value variable with no selection.
	if q.IMSI == "All" || q.IMSI == "$__all" {
		q.IMSI = ""
	}
	for name, dst := range map[string]*time.Time{"from": &q.From, "to": &q.To} {
		if s := v.Get(name); s != "" {
			ms, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return q, badRequest(name + " must be UNIX milliseconds")
			}
			*dst = time.UnixMilli(ms)
		}
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 || n > maxLimit {
			return q, badRequest("limit must be 1.." + strconv.Itoa(maxLimit))
		}
		q.Limit = n
	}
	if !q.From.Before(q.To) {
		return q, badRequest("from must be before to")
	}
	return q, nil
}

func writeJSON(w http.ResponseWriter, log *slog.Logger, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Warn("write response", "err", err)
	}
}
