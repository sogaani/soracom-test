package beam

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sogaani/soracom-test/internal/store"
)

// maxBody bounds a request body. The largest Beam body is a 65535 byte TCP
// message base64 encoded in JSON (~88 KiB).
const maxBody = 1 << 20

// Writer stores messages.
type Writer interface {
	Insert(ctx context.Context, m *store.Message) error
}

// Handler receives Beam requests on POST /beam/{source}.
type Handler struct {
	Store Writer
	// Verifier checks signatures. Nil accepts unsigned requests.
	Verifier *Verifier
	Log      *slog.Logger
	Now      func() time.Time
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	src, ok := ParseSource(r.PathValue("source"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	log := h.Log.With("source", src, "imsi", r.Header.Get(HeaderIMSI))

	if h.Verifier != nil {
		if err := h.Verifier.Verify(r.Header); err != nil {
			log.Warn("rejected request", "err", err, "remote", r.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	msg, err := Parse(src, r.Header, body, now())
	if err != nil {
		log.Warn("bad payload", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Store.Insert(r.Context(), msg); err != nil {
		log.Error("store message", "err", err)
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	log.Info("stored message")

	// The device sees this as "201 OK" over TCP/UDP. USSD only relays
	// text/plain GSM 7-bit replies.
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	if _, err := io.WriteString(w, "OK"); err != nil {
		log.Warn("write response", "err", err)
	}
}
