// Package collectors provides individual metric collectors for statsagent.
package collectors

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// SystemStats holds CPU, memory, load, and uptime metrics.
type SystemStats struct {
	Timestamp   time.Time `json:"timestamp"`
	Hostname    string    `json:"hostname"`
	UptimeSeconds float64 `json:"uptime_seconds"`

	// Load average
	LoadAvg1  float64 `json:"load_avg_1m"`
	LoadAvg5  float64 `json:"load_avg_5m"`
	LoadAvg15 float64 `json:"load_avg_15m"`

	// CPU (aggregate, 0-100)
	CPUUsedPct  float64 `json:"cpu_used_pct"`
	CPUIdlePct  float64 `json:"cpu_idle_pct"`
	CPUIowaitPct float64 `json:"cpu_iowait_pct"`
	CPUCores    int     `json:"cpu_cores"`

	// Memory (bytes)
	MemTotalBytes     int64   `json:"mem_total_bytes"`
	MemAvailableBytes int64   `json:"mem_available_bytes"`
	MemUsedBytes      int64   `json:"mem_used_bytes"`
	MemUsedPct        float64 `json:"mem_used_pct"`
	MemCachedBytes    int64   `json:"mem_cached_bytes"`
	MemBuffersBytes   int64   `json:"mem_buffers_bytes"`

	// Swap (bytes)
	SwapTotalBytes int64   `json:"swap_total_bytes"`
	SwapUsedBytes  int64   `json:"swap_used_bytes"`
	SwapUsedPct    float64 `json:"swap_used_pct"`
}

// cpuStat holds raw values from /proc/stat for delta calculation.
type cpuStat struct {
	user    uint64
	nice    uint64
	system  uint64
	idle    uint64
	iowait  uint64
	irq     uint64
	softirq uint64
}

var prevCPUStat cpuStat
var prevCPUTime time.Time

// CollectSystem reads /proc/stat, /proc/meminfo, /proc/loadavg, /proc/uptime.
func CollectSystem() (*SystemStats, error) {
	s := &SystemStats{Timestamp: time.Now()}

	hostname, _ := os.Hostname()
	s.Hostname = hostname

	if err := readUptime(s); err != nil {
		return nil, fmt.Errorf("uptime: %w", err)
	}
	if err := readLoadAvg(s); err != nil {
		return nil, fmt.Errorf("loadavg: %w", err)
	}
	if err := readMemInfo(s); err != nil {
		return nil, fmt.Errorf("meminfo: %w", err)
	}
	if err := readCPUStat(s); err != nil {
		return nil, fmt.Errorf("cpu stat: %w", err)
	}

	return s, nil
}

func readUptime(s *SystemStats) error {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return fmt.Errorf("unexpected /proc/uptime format")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return err
	}
	s.UptimeSeconds = v
	return nil
}

func readLoadAvg(s *SystemStats) error {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return fmt.Errorf("unexpected /proc/loadavg format")
	}
	s.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
	s.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
	s.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
	return nil
}

func readMemInfo(s *SystemStats) error {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return err
	}
	defer f.Close()

	values := make(map[string]int64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, _ := strconv.ParseInt(parts[1], 10, 64)
		values[key] = val * 1024 // kB → bytes
	}

	s.MemTotalBytes = values["MemTotal"]
	s.MemAvailableBytes = values["MemAvailable"]
	s.MemCachedBytes = values["Cached"] + values["SReclaimable"]
	s.MemBuffersBytes = values["Buffers"]
	s.MemUsedBytes = s.MemTotalBytes - s.MemAvailableBytes
	if s.MemTotalBytes > 0 {
		s.MemUsedPct = float64(s.MemUsedBytes) / float64(s.MemTotalBytes) * 100
	}

	s.SwapTotalBytes = values["SwapTotal"]
	s.SwapUsedBytes = values["SwapTotal"] - values["SwapFree"]
	if s.SwapTotalBytes > 0 {
		s.SwapUsedPct = float64(s.SwapUsedBytes) / float64(s.SwapTotalBytes) * 100
	}

	return scanner.Err()
}

func readCPUStat(s *SystemStats) error {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return err
	}
	defer f.Close()

	var cur cpuStat
	cores := 0
	now := time.Now()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "cpu " || strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}
			if fields[0] == "cpu" {
				cur.user, _ = strconv.ParseUint(fields[1], 10, 64)
				cur.nice, _ = strconv.ParseUint(fields[2], 10, 64)
				cur.system, _ = strconv.ParseUint(fields[3], 10, 64)
				cur.idle, _ = strconv.ParseUint(fields[4], 10, 64)
				if len(fields) > 5 {
					cur.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
				}
				if len(fields) > 6 {
					cur.irq, _ = strconv.ParseUint(fields[6], 10, 64)
				}
				if len(fields) > 7 {
					cur.softirq, _ = strconv.ParseUint(fields[7], 10, 64)
				}
			}
		} else if strings.HasPrefix(line, "cpu") {
			cores++
		}
	}

	s.CPUCores = cores

	if !prevCPUTime.IsZero() {
		elapsed := now.Sub(prevCPUTime).Seconds()
		if elapsed > 0 {
			prevTotal := prevCPUStat.user + prevCPUStat.nice + prevCPUStat.system +
				prevCPUStat.idle + prevCPUStat.iowait + prevCPUStat.irq + prevCPUStat.softirq
			curTotal := cur.user + cur.nice + cur.system +
				cur.idle + cur.iowait + cur.irq + cur.softirq

			totalDelta := float64(curTotal - prevTotal)
			idleDelta := float64(cur.idle - prevCPUStat.idle)
			iowaitDelta := float64(cur.iowait - prevCPUStat.iowait)

			if totalDelta > 0 {
				s.CPUIdlePct = idleDelta / totalDelta * 100
				s.CPUIowaitPct = iowaitDelta / totalDelta * 100
				s.CPUUsedPct = 100 - s.CPUIdlePct
			}
		}
	}

	prevCPUStat = cur
	prevCPUTime = now

	return scanner.Err()
}
