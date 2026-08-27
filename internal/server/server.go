package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jdkruzr/aragonite-loom/internal/api"
	"github.com/jdkruzr/aragonite-loom/internal/auth"
	"github.com/jdkruzr/aragonite-loom/internal/database"
	"github.com/jdkruzr/aragonite-loom/internal/tasks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Gateway struct {
	db              *sql.DB
	main            *http.Server
	spc             *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
}

func NewGateway(db *sql.DB, mainAddr, spcAddr string, shutdownTimeout time.Duration, jobEnqueuer api.JobEnqueuer, logger *slog.Logger) *Gateway {
	registry := prometheus.NewRegistry()
	metrics := NewMetrics(registry)
	mainMux := http.NewServeMux()
	mainMux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	registerProbes(mainMux, db)
	protected := http.NewServeMux()
	api.Tasks{Store: tasks.NewStore(db)}.Register(protected)
	api.Jobs{Service: jobEnqueuer}.Register(protected)
	mainMux.Handle("/api/", auth.NewStore(db).Middleware(protected))
	mainMux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"name": "Aragonite Loom", "status": "foundation"})
	})
	spcMux := http.NewServeMux()
	registerProbes(spcMux, db)
	spcMux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "SPC protocol port not yet enabled"})
	})
	return &Gateway{
		db: db, shutdownTimeout: shutdownTimeout, logger: logger,
		main: &http.Server{Addr: mainAddr, Handler: observe("main", mainMux, metrics), ReadHeaderTimeout: 10 * time.Second},
		spc:  &http.Server{Addr: spcAddr, Handler: observe("spc", spcMux, metrics), ReadHeaderTimeout: 10 * time.Second},
	}
}

func (g *Gateway) Run(ctx context.Context) error {
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for name, srv := range map[string]*http.Server{"main": g.main, "spc": g.spc} {
		wg.Add(1)
		go func(name string, srv *http.Server) {
			defer wg.Done()
			g.logger.Info("gateway listener started", "listener", name, "addr", srv.Addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errs <- fmt.Errorf("%s listener: %w", name, err)
			}
		}(name, srv)
	}
	select {
	case <-ctx.Done():
	case err := <-errs:
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), g.shutdownTimeout)
	defer cancel()
	_ = g.main.Shutdown(shutdownCtx)
	_ = g.spc.Shutdown(shutdownCtx)
	wg.Wait()
	return nil
}

func registerProbes(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := database.Ready(ctx, db); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}

func observe(listener string, next http.Handler, metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		metrics.Requests.WithLabelValues(listener, r.Method, route, strconv.Itoa(recorder.status)).Inc()
		metrics.Duration.WithLabelValues(listener, r.Method, route).Observe(time.Since(start).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
