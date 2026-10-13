package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// DockerStats holds stats for all containers on this host.
type DockerStats struct {
	Timestamp      time.Time          `json:"timestamp"`
	Available      bool               `json:"available"`
	TotalContainers int               `json:"total_containers"`
	RunningContainers int             `json:"running_containers"`
	Containers     []ContainerStats   `json:"containers"`
}

// ContainerStats holds resource usage for a single container.
type ContainerStats struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Image       string  `json:"image"`
	State       string  `json:"state"`
	CPUPct      float64 `json:"cpu_pct"`
	MemUsedBytes int64  `json:"mem_used_bytes"`
	MemLimitBytes int64 `json:"mem_limit_bytes"`
	MemPct      float64 `json:"mem_pct"`
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
	RestartCount int     `json:"restart_count"`
}

// dockerClient is a minimal HTTP client for the Docker Unix socket.
type dockerClient struct {
	client     *http.Client
	socketPath string
}

func newDockerClient(socketPath string) *dockerClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &dockerClient{
		client:     &http.Client{Transport: transport, Timeout: 5 * time.Second},
		socketPath: socketPath,
	}
}

func (d *dockerClient) get(path string) ([]byte, error) {
	resp, err := d.client.Get("http://localhost" + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// dockerContainer is the JSON shape from GET /containers/json
type dockerContainer struct {
	ID     string `json:"Id"`
	Names  []string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
}

type dockerContainerDetail struct {
	RestartCount int `json:"RestartCount"`
}

// dockerStatsResponse is the JSON from GET /containers/{id}/stats?stream=false
type dockerStatsResponse struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage int64 `json:"usage"`
		Limit int64 `json:"limit"`
		Stats struct {
			Cache int64 `json:"cache"`
		} `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
}

type prevContainerNet struct {
	rxBytes uint64
	txBytes uint64
	ts      time.Time
}

var prevContainerNets = make(map[string]prevContainerNet)

// CollectDocker collects container stats via the Docker socket.
// Returns an unavailable DockerStats if the socket isn't accessible.
func CollectDocker(socketPath string) (*DockerStats, error) {
	s := &DockerStats{Timestamp: time.Now()}

	dc := newDockerClient(socketPath)

	// List all containers (running + stopped)
	data, err := dc.get("/containers/json?all=true")
	if err != nil {
		s.Available = false
		return s, nil // not an error — Docker just isn't running
	}
	s.Available = true

	var containers []dockerContainer
	if err := json.Unmarshal(data, &containers); err != nil {
		return s, fmt.Errorf("parse container list: %w", err)
	}

	s.TotalContainers = len(containers)

	for _, c := range containers {
		name := c.ID[:12]
		if len(c.Names) > 0 {
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}

		cs := ContainerStats{
			ID:    c.ID[:12],
			Name:  name,
			Image: c.Image,
			State: c.State,
		}

		if c.State == "running" {
			s.RunningContainers++

			// Get live stats (stream=false returns one sample)
			statsData, err := dc.get("/containers/" + c.ID + "/stats?stream=false")
			if err == nil {
				cs = enrichContainerStats(cs, statsData)
			}

			// Get restart count
			detailData, err := dc.get("/containers/" + c.ID + "/json")
			if err == nil {
				var detail dockerContainerDetail
				if json.Unmarshal(detailData, &detail) == nil {
					cs.RestartCount = detail.RestartCount
				}
			}
		}

		s.Containers = append(s.Containers, cs)
	}

	return s, nil
}

func enrichContainerStats(cs ContainerStats, data []byte) ContainerStats {
	var r dockerStatsResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return cs
	}

	// CPU%
	cpuDelta := float64(r.CPUStats.CPUUsage.TotalUsage - r.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(r.CPUStats.SystemCPUUsage - r.PreCPUStats.SystemCPUUsage)
	if sysDelta > 0 && r.CPUStats.OnlineCPUs > 0 {
		cs.CPUPct = (cpuDelta / sysDelta) * float64(r.CPUStats.OnlineCPUs) * 100
	}

	// Memory
	cs.MemLimitBytes = r.MemoryStats.Limit
	// Subtract cache from usage (matches `docker stats` display)
	cs.MemUsedBytes = r.MemoryStats.Usage - r.MemoryStats.Stats.Cache
	if cs.MemLimitBytes > 0 {
		cs.MemPct = float64(cs.MemUsedBytes) / float64(cs.MemLimitBytes) * 100
	}

	// Network (sum across all interfaces)
	var rxTotal, txTotal uint64
	for _, net := range r.Networks {
		rxTotal += net.RxBytes
		txTotal += net.TxBytes
	}

	now := time.Now()
	if prev, ok := prevContainerNets[cs.ID]; ok {
		elapsed := now.Sub(prev.ts).Seconds()
		if elapsed > 0 {
			cs.RxBytesPerSec = float64(rxTotal-prev.rxBytes) / elapsed
			cs.TxBytesPerSec = float64(txTotal-prev.txBytes) / elapsed
		}
	}
	prevContainerNets[cs.ID] = prevContainerNet{rxBytes: rxTotal, txBytes: txTotal, ts: now}

	return cs
}
