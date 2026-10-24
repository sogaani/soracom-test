// Command server receives data forwarded by SORACOM Beam, stores it in
// MongoDB and serves it to Grafana.
//
// It listens on two ports:
//
//	INGEST_ADDR (:8080)  POST /beam/{http,tcp,udp,sms,ussd}  public, via the tunnel
//	API_ADDR    (:8081)  GET  /api/v1/...                     cluster-internal only
//
// Configuration (environment variables):
//
//	MONGODB_URI             required, e.g. mongodb://user:pass@mongodb:27017/soracom?authSource=soracom
//	MONGODB_DATABASE        default "soracom"
//	MONGODB_COLLECTION      default "messages"
//	BEAM_SIGNATURE_KEY      Beam pre-shared key; requests without a valid signature are rejected
//	BEAM_ALLOW_UNSIGNED     "true" to accept requests when BEAM_SIGNATURE_KEY is unset
//	BEAM_MAX_CLOCK_SKEW     max age of x-soracom-timestamp, default 2m ("0" disables)
//	INGEST_ADDR, API_ADDR   listen addresses
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sogaani/soracom-test/internal/api"
	"github.com/sogaani/soracom-test/internal/beam"
	"github.com/sogaani/soracom-test/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		return errors.New("MONGODB_URI is required")
	}
	verifier, err := newVerifier()
	if err != nil {
		return err
	}
	if verifier == nil {
		log.Warn("BEAM_SIGNATURE_KEY is not set: accepting unsigned requests")
	}

	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	st, err := store.Open(openCtx, uri, env("MONGODB_DATABASE", "soracom"), env("MONGODB_COLLECTION", "messages"))
	cancel()
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.Close(closeCtx); err != nil {
			log.Warn("close store", "err", err)
		}
	}()

	ingest := http.NewServeMux()
	beamHandler := &beam.Handler{Store: st, Verifier: verifier, Log: log}
	ingest.Handle("POST /beam/{source}", beamHandler)
	// Beam's HTTP entry point maps paths by prefix, so the device's request
	// path may be appended (e.g. /beam/http/).
	ingest.Handle("POST /beam/{source}/{rest...}", beamHandler)
	ingest.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	ingest.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Ping(r.Context()); err != nil {
			http.Error(w, "mongodb unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	servers := []*http.Server{
		newServer(env("INGEST_ADDR", ":8080"), ingest),
		newServer(env("API_ADDR", ":8081"), api.NewHandler(st, log)),
	}
	errc := make(chan error, len(servers))
	for _, s := range servers {
		log.Info("listening", "addr", s.Addr)
		go func() {
			if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err = <-errc:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		err = errors.Join(err, s.Shutdown(shutdownCtx))
	}
	return err
}

func newVerifier() (*beam.Verifier, error) {
	key := os.Getenv("BEAM_SIGNATURE_KEY")
	if key == "" {
		if os.Getenv("BEAM_ALLOW_UNSIGNED") != "true" {
			return nil, errors.New("BEAM_SIGNATURE_KEY is required (set BEAM_ALLOW_UNSIGNED=true to accept unsigned requests)")
		}
		return nil, nil
	}
	skew, err := time.ParseDuration(env("BEAM_MAX_CLOCK_SKEW", "2m"))
	if err != nil {
		return nil, fmt.Errorf("BEAM_MAX_CLOCK_SKEW: %w", err)
	}
	return &beam.Verifier{Key: []byte(key), MaxSkew: skew}, nil
}

func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
