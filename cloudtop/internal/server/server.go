// Package server implements the cloudtop metrics daemon.
//
// The server runs a background collection loop and exposes collected metrics
// over HTTP in two formats:
//
//   - Prometheus text exposition format at the configured prom_path (default /metrics)
//   - JSON snapshot at the configured json_path (default /api/v1/snapshot)
//
// An optional bearer-token auth gate can be enabled in the config.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/collector"
	"github.com/afterdarksys/opskit/cloudtop/internal/config"
	"github.com/afterdarksys/opskit/cloudtop/internal/output"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
)

// Server is the cloudtop metrics daemon.
type Server struct {
	cfg       *config.Config
	col       *collector.Collector
	providers map[string]provider.Provider

	mu       sync.RWMutex
	snapshot *Snapshot

	startTime time.Time
	httpSrv   *http.Server
}

// Snapshot is the full JSON payload returned by the snapshot endpoint.
type Snapshot struct {
	Timestamp  time.Time                   `json:"timestamp"`
	Version    string                      `json:"version"`
	UptimeSecs int64                       `json:"uptime_seconds"`
	Providers  map[string]*ProviderSection `json:"providers"`
	Errors     map[string]string           `json:"errors,omitempty"`
}

// ProviderSection holds the resources collected from one provider.
type ProviderSection struct {
	Provider  string             `json:"provider"`
	Resources []ResourceSnapshot `json:"resources"`
	Cached    bool               `json:"cached"`
	DurationMs int64             `json:"collection_duration_ms"`
}

// ResourceSnapshot is one resource from any provider.
type ResourceSnapshot struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Region  string            `json:"region,omitempty"`
	Status  string            `json:"status"`
	Tags    map[string]string `json:"tags,omitempty"`
	Metrics map[string]interface{} `json:"metrics,omitempty"`
}

// New creates a Server. Call Start to begin serving.
func New(cfg *config.Config, providers map[string]provider.Provider) *Server {
	var cache collector.Cache
	if cfg.Cache.Enabled {
		cache = collector.NewMemoryCache(cfg.Cache.TTL.Duration(), cfg.Cache.MaxSize)
	} else {
		cache = collector.NewNoopCache()
	}

	return &Server{
		cfg:       cfg,
		col:       collector.NewCollector(providers, cache),
		providers: providers,
		startTime: time.Now(),
	}
}

// Start launches the background collection goroutine and the HTTP server.
// It blocks until ctx is cancelled or a fatal listen error occurs.
func (s *Server) Start(ctx context.Context) error {
	// Perform an initial collection before accepting requests
	s.collect(ctx)

	refreshInterval := s.cfg.Server.RefreshInterval.Duration()
	if refreshInterval == 0 {
		refreshInterval = 30 * time.Second
	}

	go s.collectionLoop(ctx, refreshInterval)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/api/v1/health", s.healthHandler)

	exp := s.cfg.Server.Export
	jsonPath := exp.JSONPath
	if jsonPath == "" {
		jsonPath = "/api/v1/snapshot"
	}
	promPath := exp.PromPath
	if promPath == "" {
		promPath = "/metrics"
	}

	switch exp.Format {
	case "prometheus":
		mux.HandleFunc(promPath, s.auth(s.promHandler))
	case "json":
		mux.HandleFunc(jsonPath, s.auth(s.jsonHandler))
	default: // "both" or empty
		mux.HandleFunc(promPath, s.auth(s.promHandler))
		mux.HandleFunc(jsonPath, s.auth(s.jsonHandler))
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Shutdown when context is cancelled
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
	}()

	fmt.Printf("cloudtop server listening on %s\n", addr)
	fmt.Printf("  Prometheus metrics : http://%s%s\n", addr, promPath)
	fmt.Printf("  JSON snapshot      : http://%s%s\n", addr, jsonPath)
	fmt.Printf("  Health             : http://%s/health\n", addr)

	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// collectionLoop refreshes the snapshot on the given interval.
func (s *Server) collectionLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.collect(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// collect runs one collection pass and stores the result.
func (s *Server) collect(ctx context.Context) {
	collectCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req := &collector.CollectRequest{
		Timeout:     55 * time.Second,
		Filters:     &provider.ResourceFilter{},
		MetricTypes: []string{"host"}, // triggers GetMetrics on all providers that support it
	}

	result, err := s.col.Collect(collectCtx, req)
	if err != nil {
		// Keep stale snapshot; update error map
		s.mu.Lock()
		if s.snapshot != nil {
			s.snapshot.Errors = map[string]string{"collect": err.Error()}
		}
		s.mu.Unlock()
		return
	}

	snap := s.buildSnapshot(result)

	s.mu.Lock()
	s.snapshot = snap
	s.mu.Unlock()
}

func (s *Server) buildSnapshot(result *output.CollectResult) *Snapshot {
	snap := &Snapshot{
		Timestamp:  result.Timestamp,
		Version:    s.cfg.Version,
		UptimeSecs: int64(time.Since(s.startTime).Seconds()),
		Providers:  make(map[string]*ProviderSection),
		Errors:     make(map[string]string),
	}

	for provName, pr := range result.Results {
		section := &ProviderSection{
			Provider:   provName,
			Cached:     pr.Cached,
			DurationMs: pr.Duration.Milliseconds(),
		}
		for _, r := range pr.Resources {
			rs := ResourceSnapshot{
				ID:     r.ID,
				Name:   r.Name,
				Type:   r.Type,
				Region: r.Region,
				Status: r.Status,
				Tags:   r.Tags,
			}
			// Attach any metrics stored on the result
			if m, ok := pr.Metrics[r.ID]; ok {
				if mm, ok := m.(map[string]interface{}); ok {
					rs.Metrics = mm
				}
			}
			section.Resources = append(section.Resources, rs)
		}
		snap.Providers[provName] = section
	}

	for provName, err := range result.Errors {
		snap.Errors[provName] = err.Error()
	}

	return snap
}

// getSnapshot returns the latest snapshot (may be nil before first collection).
func (s *Server) getSnapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

// auth wraps a handler with optional bearer-token authentication.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	if !s.cfg.Server.Auth.Enabled || s.cfg.Server.Auth.Token == "" {
		return next
	}
	token := s.cfg.Server.Auth.Token
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") || strings.TrimPrefix(authHeader, "Bearer ") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// healthHandler returns a simple liveness response.
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	snap := s.getSnapshot()
	status := "starting"
	var lastCollect *time.Time
	if snap != nil {
		status = "ok"
		lastCollect = &snap.Timestamp
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"status":       status,
		"uptime_secs":  int64(time.Since(s.startTime).Seconds()),
		"last_collect": lastCollect,
		"providers":    len(s.providers),
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// jsonHandler serves the full snapshot as JSON.
func (s *Server) jsonHandler(w http.ResponseWriter, r *http.Request) {
	snap := s.getSnapshot()
	if snap == nil {
		http.Error(w, `{"error":"collection not yet complete"}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snap)
}

// promHandler serves metrics in Prometheus text exposition format.
func (s *Server) promHandler(w http.ResponseWriter, r *http.Request) {
	snap := s.getSnapshot()
	if snap == nil {
		http.Error(w, "# collection not yet complete\n", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writePrometheus(w, snap)
}
