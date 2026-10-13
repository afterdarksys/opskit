package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/afterdarksys/opskit/statsagent/output"
)

// NewServeCommand returns the `serve` subcommand.
func NewServeCommand() *cobra.Command {
	cfg := DefaultConfig()
	var port int
	var interval string
	var noDocker, noK3s, noProcess bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP metrics server (default port 9100)",
		Long: `Start an HTTP server exposing:
  GET /metrics  — Prometheus text format
  GET /stats    — JSON snapshot
  GET /health   — {"ok":true,"context":"host|docker|k3s"}`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if port > 0 {
				cfg.Port = port
			}
			if interval != "" {
				d, err := time.ParseDuration(interval)
				if err != nil {
					return fmt.Errorf("invalid interval: %w", err)
				}
				cfg.Interval = d
			}
			cfg.EnableDocker = !noDocker
			cfg.EnableK3s = !noK3s
			cfg.EnableProcess = !noProcess
			return runServe(cfg)
		},
	}

	cmd.Flags().IntVar(&port, "port", 0, "HTTP listen port (default $STATSAGENT_PORT or 9100)")
	cmd.Flags().StringVar(&interval, "interval", "", "Collection interval (e.g. 15s, 1m)")
	cmd.Flags().BoolVar(&noDocker, "no-docker", false, "Disable Docker collector")
	cmd.Flags().BoolVar(&noK3s, "no-k3s", false, "Disable k3s collector")
	cmd.Flags().BoolVar(&noProcess, "no-process", false, "Disable process collector")

	return cmd
}

func runServe(cfg *Config) error {
	ctx := detectContext()
	log.Printf("statsagent serve  port=%d  interval=%s  context=%s", cfg.Port, cfg.Interval, ctx)

	// Shared snapshot, refreshed on interval
	var mu sync.RWMutex
	var latest *output.StatsSnapshot

	// Prime collectors on startup (first sample is always delta=0)
	collect(cfg)

	// Background refresh loop
	go func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for {
			snap := collect(cfg)
			mu.Lock()
			latest = snap
			mu.Unlock()
			<-ticker.C
		}
	}()

	pw := output.NewPrometheusWriter(cfg.Labels)

	// /metrics — Prometheus
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		snap := latest
		mu.RUnlock()

		if snap == nil {
			http.Error(w, "collecting...", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var sys = snap.System
		var disk = snap.Disk
		var net = snap.Network
		var proc = snap.Process
		var docker = snap.Docker
		var k3s = snap.K3s
		pw.WriteAll(w, sys, disk, net, proc, docker, k3s)
	})

	// /stats — JSON
	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		snap := latest
		mu.RUnlock()

		if snap == nil {
			http.Error(w, `{"error":"collecting"}`, http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		output.WriteJSON(w, snap)
	})

	// /health
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hostname, _ := os.Hostname()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"context": ctx,
			"host":    hostname,
			"ts":      time.Now().Unix(),
		})
	})

	// /ready — returns 503 until first snapshot is available
	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		snap := latest
		mu.RUnlock()
		if snap == nil {
			http.Error(w, `{"ready":false}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ready":true}`)
	})

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Listening on %s", addr)
	return http.ListenAndServe(addr, nil)
}

// buildPrometheusPayload renders current metrics into a byte buffer.
func buildPrometheusPayload(snap *output.StatsSnapshot, pw *output.PrometheusWriter) []byte {
	var buf bytes.Buffer
	pw.WriteAll(&buf, snap.System, snap.Disk, snap.Network, snap.Process, snap.Docker, snap.K3s)
	return buf.Bytes()
}
