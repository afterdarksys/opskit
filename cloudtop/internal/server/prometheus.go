package server

import (
	"fmt"
	"io"
	"strings"
)

// writePrometheus writes a Snapshot in Prometheus text exposition format (0.0.4).
// Ref: https://prometheus.io/docs/instrumenting/exposition_formats/
func writePrometheus(w io.Writer, snap *Snapshot) {
	ts := snap.Timestamp.UnixMilli()

	// ---- cloudtop_up ----
	writeLine(w, "# HELP cloudtop_up Whether the cloudtop server is running (1 = up)")
	writeLine(w, "# TYPE cloudtop_up gauge")
	fmt.Fprintf(w, "cloudtop_up 1 %d\n\n", ts)

	// ---- cloudtop_uptime_seconds ----
	writeLine(w, "# HELP cloudtop_uptime_seconds Seconds since the cloudtop server started")
	writeLine(w, "# TYPE cloudtop_uptime_seconds counter")
	fmt.Fprintf(w, "cloudtop_uptime_seconds %d %d\n\n", snap.UptimeSecs, ts)

	// ---- per-provider resource counts ----
	writeLine(w, "# HELP cloudtop_resources_total Number of resources collected per provider and type")
	writeLine(w, "# TYPE cloudtop_resources_total gauge")
	typeCounts := map[string]map[string]int{} // provider -> type -> count
	for provName, section := range snap.Providers {
		typeCounts[provName] = map[string]int{}
		for _, r := range section.Resources {
			typeCounts[provName][r.Type]++
		}
	}
	for provName, types := range typeCounts {
		for rType, count := range types {
			fmt.Fprintf(w, `cloudtop_resources_total{provider=%q,type=%q} %d %d`+"\n",
				provName, rType, count, ts)
		}
	}
	writeLine(w, "")

	// ---- per-resource status ----
	writeLine(w, "# HELP cloudtop_resource_running Whether a resource is in a running/active state (1 = running)")
	writeLine(w, "# TYPE cloudtop_resource_running gauge")
	for provName, section := range snap.Providers {
		for _, r := range section.Resources {
			running := 0
			if isRunning(r.Status) {
				running = 1
			}
			region := r.Region
			if region == "" {
				region = "unknown"
			}
			fmt.Fprintf(w, `cloudtop_resource_running{provider=%q,type=%q,name=%q,region=%q} %d %d`+"\n",
				provName, r.Type, r.Name, region, running, ts)
		}
	}
	writeLine(w, "")

	// ---- per-provider collection duration ----
	writeLine(w, "# HELP cloudtop_collection_duration_ms Time taken to collect metrics from a provider in milliseconds")
	writeLine(w, "# TYPE cloudtop_collection_duration_ms gauge")
	for provName, section := range snap.Providers {
		fmt.Fprintf(w, `cloudtop_collection_duration_ms{provider=%q} %d %d`+"\n",
			provName, section.DurationMs, ts)
	}
	writeLine(w, "")

	// ---- bare metal host metrics (when present in resource metrics) ----
	writeLine(w, "# HELP cloudtop_host_cpu_usage_percent CPU utilization percentage of a bare metal host")
	writeLine(w, "# TYPE cloudtop_host_cpu_usage_percent gauge")
	writeLine(w, "# HELP cloudtop_host_memory_used_bytes Memory used by a bare metal host in bytes")
	writeLine(w, "# TYPE cloudtop_host_memory_used_bytes gauge")
	writeLine(w, "# HELP cloudtop_host_memory_total_bytes Total physical memory of a bare metal host in bytes")
	writeLine(w, "# TYPE cloudtop_host_memory_total_bytes gauge")
	writeLine(w, "# HELP cloudtop_host_disk_used_bytes Disk space used at a mount point in bytes")
	writeLine(w, "# TYPE cloudtop_host_disk_used_bytes gauge")
	writeLine(w, "# HELP cloudtop_host_disk_total_bytes Total disk space at a mount point in bytes")
	writeLine(w, "# TYPE cloudtop_host_disk_total_bytes gauge")
	writeLine(w, "# HELP cloudtop_host_network_bytes_recv Total bytes received on a network interface")
	writeLine(w, "# TYPE cloudtop_host_network_bytes_recv counter")
	writeLine(w, "# HELP cloudtop_host_network_bytes_sent Total bytes sent on a network interface")
	writeLine(w, "# TYPE cloudtop_host_network_bytes_sent counter")
	writeLine(w, "# HELP cloudtop_host_load1 1-minute load average of a bare metal host")
	writeLine(w, "# TYPE cloudtop_host_load1 gauge")

	for _, section := range snap.Providers {
		if section.Provider != "baremetal" {
			continue
		}
		for _, r := range section.Resources {
			if r.Metrics == nil {
				continue
			}
			host := r.Name

			// Try to extract the nested HostMetrics map produced by the baremetal provider.
			// The structure is: r.Metrics[hostname] = *HostMetrics (stored as map[string]interface{})
			var hostData map[string]interface{}
			if d, ok := r.Metrics[host]; ok {
				hostData, _ = d.(map[string]interface{})
			}
			if hostData == nil {
				hostData = r.Metrics
			}

			writeHostPrometheus(w, host, hostData, ts)
		}
	}
	writeLine(w, "")
}

// writeHostPrometheus emits Prometheus lines for a single host's metric map.
func writeHostPrometheus(w io.Writer, host string, m map[string]interface{}, ts int64) {
	label := fmt.Sprintf(`host=%q`, host)

	if cpu, ok := m["cpu"].(map[string]interface{}); ok {
		if v := floatVal(cpu, "usage_percent"); v >= 0 {
			fmt.Fprintf(w, "cloudtop_host_cpu_usage_percent{%s} %.2f %d\n", label, v, ts)
		}
	}

	if mem, ok := m["memory"].(map[string]interface{}); ok {
		if v := floatVal(mem, "used_bytes"); v >= 0 {
			fmt.Fprintf(w, "cloudtop_host_memory_used_bytes{%s} %.0f %d\n", label, v, ts)
		}
		if v := floatVal(mem, "total_bytes"); v >= 0 {
			fmt.Fprintf(w, "cloudtop_host_memory_total_bytes{%s} %.0f %d\n", label, v, ts)
		}
	}

	if disks, ok := m["disks"].([]interface{}); ok {
		for _, d := range disks {
			disk, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			mp := stringVal(disk, "mount_point")
			diskLabel := fmt.Sprintf(`host=%q,mount=%q`, host, mp)
			if v := floatVal(disk, "used_bytes"); v >= 0 {
				fmt.Fprintf(w, "cloudtop_host_disk_used_bytes{%s} %.0f %d\n", diskLabel, v, ts)
			}
			if v := floatVal(disk, "total_bytes"); v >= 0 {
				fmt.Fprintf(w, "cloudtop_host_disk_total_bytes{%s} %.0f %d\n", diskLabel, v, ts)
			}
		}
	}

	if nets, ok := m["network"].([]interface{}); ok {
		for _, n := range nets {
			iface, ok := n.(map[string]interface{})
			if !ok {
				continue
			}
			ifName := stringVal(iface, "interface")
			netLabel := fmt.Sprintf(`host=%q,interface=%q`, host, ifName)
			if v := floatVal(iface, "bytes_recv"); v >= 0 {
				fmt.Fprintf(w, "cloudtop_host_network_bytes_recv{%s} %.0f %d\n", netLabel, v, ts)
			}
			if v := floatVal(iface, "bytes_sent"); v >= 0 {
				fmt.Fprintf(w, "cloudtop_host_network_bytes_sent{%s} %.0f %d\n", netLabel, v, ts)
			}
		}
	}

	if load, ok := m["load"].(map[string]interface{}); ok {
		if v := floatVal(load, "load1"); v >= 0 {
			fmt.Fprintf(w, "cloudtop_host_load1{%s} %.2f %d\n", label, v, ts)
		}
	}
}

func writeLine(w io.Writer, s string) {
	fmt.Fprintln(w, s)
}

func floatVal(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return -1
	}
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case int:
		return float64(t)
	}
	return -1
}

func stringVal(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func isRunning(status string) bool {
	s := strings.ToLower(status)
	return s == "running" || s == "active" || s == "up" || s == "healthy"
}
