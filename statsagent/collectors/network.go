package collectors

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// NetworkStats holds per-interface network counters.
type NetworkStats struct {
	Timestamp  time.Time       `json:"timestamp"`
	Interfaces []InterfaceStat `json:"interfaces"`
}

// InterfaceStat holds deltas (per-second rates) for one NIC.
type InterfaceStat struct {
	Name        string  `json:"name"`
	RxBytesPerSec  float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec  float64 `json:"tx_bytes_per_sec"`
	RxPktsPerSec   float64 `json:"rx_pkts_per_sec"`
	TxPktsPerSec   float64 `json:"tx_pkts_per_sec"`
	RxErrors    uint64  `json:"rx_errors"`
	TxErrors    uint64  `json:"tx_errors"`
	RxDropped   uint64  `json:"rx_dropped"`
	TxDropped   uint64  `json:"tx_dropped"`
	// Cumulative totals for reference
	RxTotalBytes uint64 `json:"rx_total_bytes"`
	TxTotalBytes uint64 `json:"tx_total_bytes"`
}

type rawNetStat struct {
	rxBytes  uint64
	txBytes  uint64
	rxPkts   uint64
	txPkts   uint64
	rxErrors uint64
	txErrors uint64
	rxDropped uint64
	txDropped uint64
	ts       time.Time
}

var prevNetStats = make(map[string]rawNetStat)

// skipInterfaces are virtual/loopback interfaces.
var skipInterfaces = map[string]bool{
	"lo": true,
}

// CollectNetwork reads /proc/net/dev and returns per-interface rate stats.
func CollectNetwork() (*NetworkStats, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, fmt.Errorf("open /proc/net/dev: %w", err)
	}
	defer f.Close()

	now := time.Now()
	s := &NetworkStats{Timestamp: now}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 { // skip header lines
			continue
		}

		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}

		name := strings.TrimSpace(line[:colonIdx])
		if skipInterfaces[name] {
			continue
		}
		// Skip docker bridge and veth interfaces (container-specific)
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") ||
			strings.HasPrefix(name, "br-") {
			continue
		}

		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 16 {
			continue
		}

		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		rxPkts, _ := strconv.ParseUint(fields[1], 10, 64)
		rxErrors, _ := strconv.ParseUint(fields[2], 10, 64)
		rxDropped, _ := strconv.ParseUint(fields[3], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		txPkts, _ := strconv.ParseUint(fields[9], 10, 64)
		txErrors, _ := strconv.ParseUint(fields[10], 10, 64)
		txDropped, _ := strconv.ParseUint(fields[11], 10, 64)

		cur := rawNetStat{
			rxBytes: rxBytes, txBytes: txBytes,
			rxPkts: rxPkts, txPkts: txPkts,
			rxErrors: rxErrors, txErrors: txErrors,
			rxDropped: rxDropped, txDropped: txDropped,
			ts: now,
		}

		iface := InterfaceStat{
			Name:         name,
			RxTotalBytes: rxBytes,
			TxTotalBytes: txBytes,
			RxErrors:     rxErrors,
			TxErrors:     txErrors,
			RxDropped:    rxDropped,
			TxDropped:    txDropped,
		}

		if prev, ok := prevNetStats[name]; ok {
			elapsed := cur.ts.Sub(prev.ts).Seconds()
			if elapsed > 0 {
				iface.RxBytesPerSec = float64(cur.rxBytes-prev.rxBytes) / elapsed
				iface.TxBytesPerSec = float64(cur.txBytes-prev.txBytes) / elapsed
				iface.RxPktsPerSec = float64(cur.rxPkts-prev.rxPkts) / elapsed
				iface.TxPktsPerSec = float64(cur.txPkts-prev.txPkts) / elapsed
			}
		}

		prevNetStats[name] = cur
		s.Interfaces = append(s.Interfaces, iface)
	}

	return s, scanner.Err()
}
