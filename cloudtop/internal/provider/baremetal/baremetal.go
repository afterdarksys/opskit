// Package baremetal implements a provider for bare metal and dedicated VM hosts.
// It collects OS-level metrics (CPU, memory, disk, network) directly from the
// host using syscalls and /proc on Linux. On other platforms metrics are
// stubbed but the provider still registers successfully.
package baremetal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/metrics"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
)

func init() {
	provider.Register("baremetal", func() provider.Provider {
		return &BaremetalProvider{}
	})
}

// BaremetalProvider monitors bare metal / dedicated VM hosts.
type BaremetalProvider struct {
	config *provider.ProviderConfig
	hosts  []hostEntry
}

type hostEntry struct {
	name   string
	labels map[string]string
	// future: ssh connection config for remote hosts
}

func (p *BaremetalProvider) Name() string { return "baremetal" }

func (p *BaremetalProvider) Initialize(_ context.Context, cfg *provider.ProviderConfig) error {
	p.config = cfg

	// Parse host list from options["hosts"]; default to localhost
	if raw, ok := cfg.Options["hosts"]; ok {
		if list, ok := raw.([]interface{}); ok {
			for _, item := range list {
				if m, ok := item.(map[string]interface{}); ok {
					entry := hostEntry{name: "localhost"}
					if n, ok := m["name"].(string); ok {
						entry.name = n
					}
					if labels, ok := m["labels"].(map[string]interface{}); ok {
						entry.labels = make(map[string]string)
						for k, v := range labels {
							entry.labels[k] = fmt.Sprint(v)
						}
					}
					p.hosts = append(p.hosts, entry)
				}
			}
		}
	}

	if len(p.hosts) == 0 {
		hostname, _ := os.Hostname()
		p.hosts = []hostEntry{{name: hostname}}
	}

	return nil
}

func (p *BaremetalProvider) HealthCheck(_ context.Context) error {
	// Local collection is always available
	return nil
}

func (p *BaremetalProvider) ListServices(_ context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "host", Name: "Host Metrics", Type: "compute", Capabilities: []string{"cpu", "memory", "disk", "network"}},
	}, nil
}

func (p *BaremetalProvider) ListResources(_ context.Context, _ *provider.ResourceFilter) ([]provider.Resource, error) {
	now := time.Now()
	resources := make([]provider.Resource, 0, len(p.hosts))

	for _, h := range p.hosts {
		tags := map[string]string{
			"os":   runtime.GOOS,
			"arch": runtime.GOARCH,
		}
		for k, v := range h.labels {
			tags[k] = v
		}

		resources = append(resources, provider.Resource{
			ID:        h.name,
			Name:      h.name,
			Type:      "host",
			Provider:  "baremetal",
			Region:    "local",
			Status:    "running",
			Tags:      tags,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	return resources, nil
}

func (p *BaremetalProvider) GetMetrics(_ context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	result := make(map[string]interface{})

	for _, id := range req.ResourceIDs {
		hostMetrics, err := collectHostMetrics(id)
		if err != nil {
			continue
		}
		// Marshal to map[string]interface{} so the server can encode it as JSON
		// and the Prometheus exporter can navigate the fields generically.
		m, err := structToMap(hostMetrics)
		if err != nil {
			continue
		}
		result[id] = m
	}

	return &provider.MetricsResponse{
		Provider:  "baremetal",
		Metrics:   result,
		Timestamp: time.Now(),
	}, nil
}

// structToMap converts any value to map[string]interface{} via JSON round-trip.
func structToMap(v interface{}) (map[string]interface{}, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (p *BaremetalProvider) Close() error { return nil }

// HostMetrics is the full metric snapshot for one host.
type HostMetrics struct {
	Hostname  string          `json:"hostname"`
	OS        string          `json:"os"`
	Arch      string          `json:"arch"`
	Timestamp time.Time       `json:"timestamp"`
	CPU       CPUStats        `json:"cpu"`
	Memory    MemoryStats     `json:"memory"`
	Disks     []DiskStats     `json:"disks"`
	Network   []NetworkStats  `json:"network"`
	Load      LoadStats       `json:"load"`
}

// CPUStats holds CPU utilization data.
type CPUStats struct {
	Cores        int     `json:"cores"`
	UsagePercent float64 `json:"usage_percent"`
}

// MemoryStats holds memory utilization data.
type MemoryStats struct {
	TotalBytes     int64   `json:"total_bytes"`
	UsedBytes      int64   `json:"used_bytes"`
	AvailableBytes int64   `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
	CachedBytes    int64   `json:"cached_bytes,omitempty"`
	BuffersBytes   int64   `json:"buffers_bytes,omitempty"`
}

// DiskStats holds per-mount-point disk usage.
type DiskStats struct {
	MountPoint   string  `json:"mount_point"`
	Device       string  `json:"device,omitempty"`
	TotalBytes   int64   `json:"total_bytes"`
	UsedBytes    int64   `json:"used_bytes"`
	FreeBytes    int64   `json:"free_bytes"`
	UsagePercent float64 `json:"usage_percent"`
	FSType       string  `json:"fs_type,omitempty"`
}

// NetworkStats holds per-interface network counters.
type NetworkStats struct {
	Interface   string `json:"interface"`
	BytesRecv   int64  `json:"bytes_recv"`
	BytesSent   int64  `json:"bytes_sent"`
	PacketsRecv int64  `json:"packets_recv"`
	PacketsSent int64  `json:"packets_sent"`
	ErrIn       int64  `json:"errors_in,omitempty"`
	ErrOut      int64  `json:"errors_out,omitempty"`
}

// LoadStats holds system load averages.
type LoadStats struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// collectHostMetrics gathers all metrics for the local host.
// Platform-specific implementations are in collect_linux.go / collect_other.go.
func collectHostMetrics(hostname string) (*HostMetrics, error) {
	h := &HostMetrics{
		Hostname:  hostname,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Timestamp: time.Now(),
		CPU: CPUStats{
			Cores: runtime.NumCPU(),
		},
	}

	if err := fillMetrics(h); err != nil {
		return h, err
	}

	return h, nil
}

// CollectComputeMetrics converts HostMetrics into the standard ComputeMetrics type.
func (h *HostMetrics) CollectComputeMetrics() *metrics.ComputeMetrics {
	m := &metrics.ComputeMetrics{
		ResourceID:          h.Hostname,
		Provider:            "baremetal",
		Timestamp:           h.Timestamp,
		CPUUsagePercent:     h.CPU.UsagePercent,
		CPUCores:            h.CPU.Cores,
		MemoryUsedBytes:     h.Memory.UsedBytes,
		MemoryTotalBytes:    h.Memory.TotalBytes,
		MemoryUsagePercent:  h.Memory.UsagePercent,
	}
	if len(h.Network) > 0 {
		m.NetworkInBytesPerSec = h.Network[0].BytesRecv
		m.NetworkOutBytesPerSec = h.Network[0].BytesSent
	}
	return m
}
