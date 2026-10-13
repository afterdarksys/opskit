//go:build linux

package baremetal

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func fillMetrics(h *HostMetrics) error {
	if err := fillCPU(h); err != nil {
		return fmt.Errorf("cpu: %w", err)
	}
	fillMemory(h)
	fillDisks(h)
	fillNetwork(h)
	fillLoad(h)
	return nil
}

// fillCPU samples /proc/stat twice ~100ms apart to compute usage.
func fillCPU(h *HostMetrics) error {
	s1, err := readCPUStat()
	if err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	s2, err := readCPUStat()
	if err != nil {
		return err
	}

	idle1 := s1[3]
	idle2 := s2[3]
	total1 := sumUint64(s1)
	total2 := sumUint64(s2)

	totalDiff := float64(total2 - total1)
	idleDiff := float64(idle2 - idle1)

	if totalDiff > 0 {
		h.CPU.UsagePercent = (1.0 - idleDiff/totalDiff) * 100.0
	}
	return nil
}

func readCPUStat() ([]uint64, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		vals := make([]uint64, 0, len(fields)-1)
		for _, field := range fields[1:] {
			v, _ := strconv.ParseUint(field, 10, 64)
			vals = append(vals, v)
		}
		return vals, nil
	}
	return nil, fmt.Errorf("cpu line not found in /proc/stat")
}

func sumUint64(vals []uint64) uint64 {
	var total uint64
	for _, v := range vals {
		total += v
	}
	return total
}

// fillMemory reads /proc/meminfo.
func fillMemory(h *HostMetrics) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()

	m := make(map[string]int64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		m[key] = val * 1024 // kB → bytes
	}

	total := m["MemTotal"]
	available := m["MemAvailable"]
	used := total - available

	h.Memory = MemoryStats{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		CachedBytes:    m["Cached"],
		BuffersBytes:   m["Buffers"],
	}
	if total > 0 {
		h.Memory.UsagePercent = float64(used) / float64(total) * 100.0
	}
}

// fillDisks reads disk usage for common mount points using statfs.
func fillDisks(h *HostMetrics) {
	mounts := readMountPoints()
	for _, mp := range mounts {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mp.mountpoint, &stat); err != nil {
			continue
		}
		total := int64(stat.Blocks) * stat.Bsize
		free := int64(stat.Bfree) * stat.Bsize
		used := total - free
		var pct float64
		if total > 0 {
			pct = float64(used) / float64(total) * 100.0
		}
		h.Disks = append(h.Disks, DiskStats{
			MountPoint:   mp.mountpoint,
			Device:       mp.device,
			FSType:       mp.fstype,
			TotalBytes:   total,
			UsedBytes:    used,
			FreeBytes:    free,
			UsagePercent: pct,
		})
	}
}

type mountInfo struct {
	device     string
	mountpoint string
	fstype     string
}

func readMountPoints() []mountInfo {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()

	skip := map[string]bool{
		"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
		"tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
		"securityfs": true, "hugetlbfs": true, "mqueue": true, "debugfs": true,
		"tracefs": true, "fusectl": true, "configfs": true, "bpf": true,
		"autofs": true, "efivarfs": true,
	}

	var mounts []mountInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		fstype := fields[2]
		if skip[fstype] {
			continue
		}
		mounts = append(mounts, mountInfo{
			device:     fields[0],
			mountpoint: fields[1],
			fstype:     fstype,
		})
	}
	return mounts
}

// fillNetwork reads /proc/net/dev for interface counters.
func fillNetwork(h *HostMetrics) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Skip 2 header lines
	scanner.Scan()
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:idx])
		// Skip loopback
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			continue
		}
		ns := NetworkStats{Interface: iface}
		ns.BytesRecv, _ = strconv.ParseInt(fields[0], 10, 64)
		ns.PacketsRecv, _ = strconv.ParseInt(fields[1], 10, 64)
		ns.ErrIn, _ = strconv.ParseInt(fields[2], 10, 64)
		ns.BytesSent, _ = strconv.ParseInt(fields[8], 10, 64)
		ns.PacketsSent, _ = strconv.ParseInt(fields[9], 10, 64)
		ns.ErrOut, _ = strconv.ParseInt(fields[10], 10, 64)
		h.Network = append(h.Network, ns)
	}
}

// fillLoad reads load averages from sysinfo syscall.
func fillLoad(h *HostMetrics) {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return
	}
	scale := float64(1 << 16)
	h.Load = LoadStats{
		Load1:  float64(info.Loads[0]) / scale,
		Load5:  float64(info.Loads[1]) / scale,
		Load15: float64(info.Loads[2]) / scale,
	}
}
