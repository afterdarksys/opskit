package collectors

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ProcessStats holds aggregate and top-N process information.
type ProcessStats struct {
	Timestamp   time.Time    `json:"timestamp"`
	TotalProcs  int          `json:"total_procs"`
	RunningProcs int         `json:"running_procs"`
	ZombieProcs int          `json:"zombie_procs"`
	TopCPU      []ProcInfo   `json:"top_cpu"`
	TopMem      []ProcInfo   `json:"top_mem"`
}

// ProcInfo holds information about a single process.
type ProcInfo struct {
	PID     int     `json:"pid"`
	Name    string  `json:"name"`
	State   string  `json:"state"`
	CPUPct  float64 `json:"cpu_pct"`
	MemRSSBytes int64 `json:"mem_rss_bytes"`
	MemPct  float64 `json:"mem_pct"`
}

type rawProcStat struct {
	utime  uint64
	stime  uint64
	ts     time.Time
}

var prevProcStats = make(map[int]rawProcStat)
var systemMemTotal int64

// CollectProcess reads /proc/[pid]/stat for all processes.
func CollectProcess(topN int) (*ProcessStats, error) {
	if topN <= 0 {
		topN = 10
	}

	now := time.Now()
	s := &ProcessStats{Timestamp: now}

	// Get total memory for percent calc
	if systemMemTotal == 0 {
		memStats, err := CollectSystem()
		if err == nil {
			systemMemTotal = memStats.MemTotalBytes
		}
	}

	// Get clock ticks per second
	clkTck := int64(100) // default Hz, works for Linux

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("readdir /proc: %w", err)
	}

	var procs []ProcInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		info, err := readProcStat(pid, now, clkTck)
		if err != nil {
			continue
		}

		switch info.State {
		case "R":
			s.RunningProcs++
		case "Z":
			s.ZombieProcs++
		}
		s.TotalProcs++

		if systemMemTotal > 0 {
			info.MemPct = float64(info.MemRSSBytes) / float64(systemMemTotal) * 100
		}

		procs = append(procs, info)
	}

	// Sort by CPU descending, take top N
	cpuSorted := make([]ProcInfo, len(procs))
	copy(cpuSorted, procs)
	sort.Slice(cpuSorted, func(i, j int) bool {
		return cpuSorted[i].CPUPct > cpuSorted[j].CPUPct
	})
	if len(cpuSorted) > topN {
		cpuSorted = cpuSorted[:topN]
	}
	s.TopCPU = cpuSorted

	// Sort by memory descending, take top N
	memSorted := make([]ProcInfo, len(procs))
	copy(memSorted, procs)
	sort.Slice(memSorted, func(i, j int) bool {
		return memSorted[i].MemRSSBytes > memSorted[j].MemRSSBytes
	})
	if len(memSorted) > topN {
		memSorted = memSorted[:topN]
	}
	s.TopMem = memSorted

	return s, nil
}

func readProcStat(pid int, now time.Time, clkTck int64) (ProcInfo, error) {
	info := ProcInfo{PID: pid}

	// Read /proc/[pid]/stat
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return info, err
	}

	// Format: pid (comm) state ... utime stime ...
	// comm may contain spaces and parens — parse carefully
	raw := string(data)
	commStart := strings.Index(raw, "(")
	commEnd := strings.LastIndex(raw, ")")
	if commStart < 0 || commEnd < 0 {
		return info, fmt.Errorf("malformed stat")
	}

	info.Name = raw[commStart+1 : commEnd]

	rest := strings.Fields(raw[commEnd+2:])
	if len(rest) < 13 {
		return info, fmt.Errorf("too few fields")
	}

	info.State = rest[0]
	utime, _ := strconv.ParseUint(rest[11], 10, 64) // field 14 (0-indexed from after comm+state)
	stime, _ := strconv.ParseUint(rest[12], 10, 64) // field 15

	// Calculate CPU%
	cur := rawProcStat{utime: utime, stime: stime, ts: now}
	if prev, ok := prevProcStats[pid]; ok {
		elapsed := cur.ts.Sub(prev.ts).Seconds()
		if elapsed > 0 {
			ticksDelta := float64((cur.utime + cur.stime) - (prev.utime + prev.stime))
			info.CPUPct = (ticksDelta / float64(clkTck)) / elapsed * 100
		}
	}
	prevProcStats[pid] = cur

	// Read RSS from /proc/[pid]/status
	info.MemRSSBytes = readProcRSS(pid)

	return info, nil
}

func readProcRSS(pid int) int64 {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}
