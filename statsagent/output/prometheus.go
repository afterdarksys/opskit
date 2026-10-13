// Package output provides Prometheus and JSON formatters for statsagent metrics.
package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/afterdarksys/opskit/statsagent/collectors"
)

// PrometheusWriter writes metrics in Prometheus text exposition format.
type PrometheusWriter struct {
	labels string // pre-formatted label string, e.g. {host="foo",env="prod"}
}

// NewPrometheusWriter creates a writer with the given extra labels.
func NewPrometheusWriter(extraLabels map[string]string) *PrometheusWriter {
	var parts []string
	for k, v := range extraLabels {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, sanitizeLabel(v)))
	}
	ls := ""
	if len(parts) > 0 {
		ls = "{" + strings.Join(parts, ",") + "}"
	}
	return &PrometheusWriter{labels: ls}
}

func (pw *PrometheusWriter) l(extra ...string) string {
	if pw.labels == "" && len(extra) == 0 {
		return ""
	}
	if pw.labels == "" {
		return "{" + strings.Join(extra, ",") + "}"
	}
	base := pw.labels[:len(pw.labels)-1] // strip closing }
	if len(extra) > 0 {
		return base + "," + strings.Join(extra, ",") + "}"
	}
	return pw.labels
}

func (pw *PrometheusWriter) WriteAll(w io.Writer,
	sys *collectors.SystemStats,
	disk *collectors.DiskStats,
	net *collectors.NetworkStats,
	proc *collectors.ProcessStats,
	docker *collectors.DockerStats,
	k3s *collectors.K3sStats,
) {
	if sys != nil {
		pw.writeSystem(w, sys)
	}
	if disk != nil {
		pw.writeDisk(w, disk)
	}
	if net != nil {
		pw.writeNetwork(w, net)
	}
	if proc != nil {
		pw.writeProcess(w, proc)
	}
	if docker != nil {
		pw.writeDocker(w, docker)
	}
	if k3s != nil {
		pw.writeK3s(w, k3s)
	}

	// Scrape timestamp
	fmt.Fprintf(w, "# statsagent_scrape_timestamp_seconds %d\n", time.Now().Unix())
}

func (pw *PrometheusWriter) writeSystem(w io.Writer, s *collectors.SystemStats) {
	l := pw.labels
	hostLabel := fmt.Sprintf(`host="%s"`, s.Hostname)
	lh := pw.l(hostLabel)

	gauge(w, "statsagent_uptime_seconds", "System uptime in seconds", lh, s.UptimeSeconds)
	gauge(w, "statsagent_load_avg_1m", "1-minute load average", lh, s.LoadAvg1)
	gauge(w, "statsagent_load_avg_5m", "5-minute load average", lh, s.LoadAvg5)
	gauge(w, "statsagent_load_avg_15m", "15-minute load average", lh, s.LoadAvg15)
	gauge(w, "statsagent_cpu_used_pct", "CPU usage percent", lh, s.CPUUsedPct)
	gauge(w, "statsagent_cpu_idle_pct", "CPU idle percent", lh, s.CPUIdlePct)
	gauge(w, "statsagent_cpu_iowait_pct", "CPU iowait percent", lh, s.CPUIowaitPct)
	gauge(w, "statsagent_cpu_cores", "Number of CPU cores", lh, float64(s.CPUCores))
	gauge(w, "statsagent_mem_total_bytes", "Total memory bytes", lh, float64(s.MemTotalBytes))
	gauge(w, "statsagent_mem_available_bytes", "Available memory bytes", lh, float64(s.MemAvailableBytes))
	gauge(w, "statsagent_mem_used_bytes", "Used memory bytes", lh, float64(s.MemUsedBytes))
	gauge(w, "statsagent_mem_used_pct", "Memory used percent", lh, s.MemUsedPct)
	gauge(w, "statsagent_mem_cached_bytes", "Cached memory bytes", lh, float64(s.MemCachedBytes))
	gauge(w, "statsagent_swap_total_bytes", "Total swap bytes", lh, float64(s.SwapTotalBytes))
	gauge(w, "statsagent_swap_used_bytes", "Used swap bytes", lh, float64(s.SwapUsedBytes))
	gauge(w, "statsagent_swap_used_pct", "Swap used percent", lh, s.SwapUsedPct)
	_ = l
}

func (pw *PrometheusWriter) writeDisk(w io.Writer, s *collectors.DiskStats) {
	for _, m := range s.Mounts {
		ls := pw.l(
			fmt.Sprintf(`device="%s"`, sanitizeLabel(m.Device)),
			fmt.Sprintf(`mount="%s"`, sanitizeLabel(m.MountPoint)),
			fmt.Sprintf(`fstype="%s"`, sanitizeLabel(m.FSType)),
		)
		gauge(w, "statsagent_disk_total_bytes", "Disk total bytes", ls, float64(m.TotalBytes))
		gauge(w, "statsagent_disk_used_bytes", "Disk used bytes", ls, float64(m.UsedBytes))
		gauge(w, "statsagent_disk_free_bytes", "Disk free bytes", ls, float64(m.FreeBytes))
		gauge(w, "statsagent_disk_used_pct", "Disk used percent", ls, m.UsedPct)
	}
	for _, d := range s.Devices {
		ls := pw.l(fmt.Sprintf(`device="%s"`, sanitizeLabel(d.Device)))
		gauge(w, "statsagent_disk_reads_per_sec", "Disk reads per second", ls, d.ReadsPerSec)
		gauge(w, "statsagent_disk_writes_per_sec", "Disk writes per second", ls, d.WritesPerSec)
		gauge(w, "statsagent_disk_read_bytes_per_sec", "Disk read bytes per second", ls, d.ReadBytesPerSec)
		gauge(w, "statsagent_disk_write_bytes_per_sec", "Disk write bytes per second", ls, d.WriteBytesPerSec)
		gauge(w, "statsagent_disk_util_pct", "Disk utilization percent", ls, d.UtilPct)
	}
}

func (pw *PrometheusWriter) writeNetwork(w io.Writer, s *collectors.NetworkStats) {
	for _, iface := range s.Interfaces {
		ls := pw.l(fmt.Sprintf(`interface="%s"`, sanitizeLabel(iface.Name)))
		gauge(w, "statsagent_net_rx_bytes_per_sec", "Network RX bytes/sec", ls, iface.RxBytesPerSec)
		gauge(w, "statsagent_net_tx_bytes_per_sec", "Network TX bytes/sec", ls, iface.TxBytesPerSec)
		gauge(w, "statsagent_net_rx_pkts_per_sec", "Network RX packets/sec", ls, iface.RxPktsPerSec)
		gauge(w, "statsagent_net_tx_pkts_per_sec", "Network TX packets/sec", ls, iface.TxPktsPerSec)
		counter(w, "statsagent_net_rx_errors_total", "Network RX errors", ls, float64(iface.RxErrors))
		counter(w, "statsagent_net_tx_errors_total", "Network TX errors", ls, float64(iface.TxErrors))
		counter(w, "statsagent_net_rx_dropped_total", "Network RX dropped", ls, float64(iface.RxDropped))
		counter(w, "statsagent_net_tx_dropped_total", "Network TX dropped", ls, float64(iface.TxDropped))
	}
}

func (pw *PrometheusWriter) writeProcess(w io.Writer, s *collectors.ProcessStats) {
	l := pw.labels
	gauge(w, "statsagent_proc_total", "Total number of processes", l, float64(s.TotalProcs))
	gauge(w, "statsagent_proc_running", "Running processes", l, float64(s.RunningProcs))
	gauge(w, "statsagent_proc_zombie", "Zombie processes", l, float64(s.ZombieProcs))
}

func (pw *PrometheusWriter) writeDocker(w io.Writer, s *collectors.DockerStats) {
	l := pw.labels
	avail := 0.0
	if s.Available {
		avail = 1.0
	}
	gauge(w, "statsagent_docker_available", "Docker daemon available (1=yes)", l, avail)
	if !s.Available {
		return
	}
	gauge(w, "statsagent_docker_containers_total", "Total containers", l, float64(s.TotalContainers))
	gauge(w, "statsagent_docker_containers_running", "Running containers", l, float64(s.RunningContainers))

	for _, c := range s.Containers {
		ls := pw.l(
			fmt.Sprintf(`container="%s"`, sanitizeLabel(c.Name)),
			fmt.Sprintf(`image="%s"`, sanitizeLabel(c.Image)),
			fmt.Sprintf(`state="%s"`, c.State),
		)
		gauge(w, "statsagent_docker_container_cpu_pct", "Container CPU percent", ls, c.CPUPct)
		gauge(w, "statsagent_docker_container_mem_used_bytes", "Container memory used bytes", ls, float64(c.MemUsedBytes))
		gauge(w, "statsagent_docker_container_mem_limit_bytes", "Container memory limit bytes", ls, float64(c.MemLimitBytes))
		gauge(w, "statsagent_docker_container_mem_pct", "Container memory percent", ls, c.MemPct)
		gauge(w, "statsagent_docker_container_rx_bytes_per_sec", "Container network RX bytes/sec", ls, c.RxBytesPerSec)
		gauge(w, "statsagent_docker_container_tx_bytes_per_sec", "Container network TX bytes/sec", ls, c.TxBytesPerSec)
		gauge(w, "statsagent_docker_container_restarts_total", "Container restart count", ls, float64(c.RestartCount))
	}
}

func (pw *PrometheusWriter) writeK3s(w io.Writer, s *collectors.K3sStats) {
	l := pw.labels
	avail := 0.0
	if s.Available {
		avail = 1.0
	}
	gauge(w, "statsagent_k3s_available", "k3s API reachable (1=yes)", l, avail)
	if !s.Available {
		return
	}
	gauge(w, "statsagent_k3s_nodes_total", "Total k3s nodes", l, float64(s.TotalNodes))
	gauge(w, "statsagent_k3s_nodes_ready", "Ready k3s nodes", l, float64(s.ReadyNodes))
	gauge(w, "statsagent_k3s_pods_total", "Total pods", l, float64(s.TotalPods))
	gauge(w, "statsagent_k3s_pods_running", "Running pods", l, float64(s.RunningPods))
	gauge(w, "statsagent_k3s_pods_failed", "Failed pods", l, float64(s.FailedPods))

	for _, ns := range s.Namespaces {
		ls := pw.l(fmt.Sprintf(`namespace="%s"`, sanitizeLabel(ns.Name)))
		gauge(w, "statsagent_k3s_ns_pods_total", "Pods in namespace", ls, float64(ns.TotalPods))
		gauge(w, "statsagent_k3s_ns_pods_running", "Running pods in namespace", ls, float64(ns.RunningPods))
	}
}

// --- helpers ---

func gauge(w io.Writer, name, help, labels string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s%s %g\n", name, help, name, name, labels, value)
}

func counter(w io.Writer, name, help, labels string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s%s %g\n", name, help, name, name, labels, value)
}

func sanitizeLabel(s string) string {
	return strings.NewReplacer(`"`, `\"`, "\n", "\\n", `\`, `\\`).Replace(s)
}
