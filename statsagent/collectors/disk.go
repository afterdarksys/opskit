package collectors

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DiskStats holds disk usage and I/O stats.
type DiskStats struct {
	Timestamp time.Time    `json:"timestamp"`
	Mounts    []MountStats `json:"mounts"`
	Devices   []IOStats    `json:"devices"`
}

// MountStats holds usage for a single mount point.
type MountStats struct {
	Device     string  `json:"device"`
	MountPoint string  `json:"mount_point"`
	FSType     string  `json:"fstype"`
	TotalBytes int64   `json:"total_bytes"`
	UsedBytes  int64   `json:"used_bytes"`
	FreeBytes  int64   `json:"free_bytes"`
	UsedPct    float64 `json:"used_pct"`
}

// IOStats holds block I/O counters for a device (deltas from last sample).
type IOStats struct {
	Device         string  `json:"device"`
	ReadsPerSec    float64 `json:"reads_per_sec"`
	WritesPerSec   float64 `json:"writes_per_sec"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
	AwaitMs        float64 `json:"await_ms"`
	UtilPct        float64 `json:"util_pct"`
}

// rawIOStat holds raw /proc/diskstats fields for delta calculation.
type rawIOStat struct {
	reads      uint64
	readBytes  uint64
	writes     uint64
	writeBytes uint64
	ioTicks    uint64
	ts         time.Time
}

var prevIOStats = make(map[string]rawIOStat)

// CollectDisk collects mount usage from /proc/mounts and I/O from /proc/diskstats.
func CollectDisk() (*DiskStats, error) {
	s := &DiskStats{Timestamp: time.Now()}

	mounts, err := collectMounts()
	if err != nil {
		return nil, fmt.Errorf("mounts: %w", err)
	}
	s.Mounts = mounts

	devices, err := collectDiskIO()
	if err != nil {
		// Non-fatal — diskstats may not be available
		devices = []IOStats{}
	}
	s.Devices = devices

	return s, nil
}

// skipFSTypes are virtual/pseudo filesystems we don't want to stat.
var skipFSTypes = map[string]bool{
	"proc":      true,
	"sysfs":     true,
	"devpts":    true,
	"devtmpfs":  true,
	"cgroup":    true,
	"cgroup2":   true,
	"tmpfs":     true,
	"hugetlbfs": true,
	"mqueue":    true,
	"pstore":    true,
	"securityfs":true,
	"debugfs":   true,
	"tracefs":   true,
	"bpf":       true,
	"fusectl":   true,
	"overlay":   true, // docker layers — report at container level
	"squashfs":  true,
}

func collectMounts() ([]MountStats, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := make(map[string]bool)
	var mounts []MountStats

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		if skipFSTypes[fsType] {
			continue
		}
		if seen[mountPoint] {
			continue
		}
		seen[mountPoint] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}

		total := int64(stat.Blocks) * int64(stat.Bsize)
		free := int64(stat.Bfree) * int64(stat.Bsize)
		used := total - free

		var usedPct float64
		if total > 0 {
			usedPct = float64(used) / float64(total) * 100
		}

		mounts = append(mounts, MountStats{
			Device:     device,
			MountPoint: mountPoint,
			FSType:     fsType,
			TotalBytes: total,
			UsedBytes:  used,
			FreeBytes:  free,
			UsedPct:    usedPct,
		})
	}

	return mounts, scanner.Err()
}

func collectDiskIO() ([]IOStats, error) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	now := time.Now()
	var result []IOStats

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Format: major minor name reads_completed reads_merged sectors_read ms_reading
		//         writes_completed writes_merged sectors_written ms_writing
		//         ios_in_progress ms_doing_io ms_weighted_io
		if len(fields) < 14 {
			continue
		}

		name := fields[2]

		// Skip partitions (only track whole disks: sda, vda, nvme0n1, etc.)
		// A partition ends with a digit preceded by a letter or 'n' for NVMe.
		if isPartition(name) {
			continue
		}

		reads, _ := strconv.ParseUint(fields[3], 10, 64)
		sectorsRead, _ := strconv.ParseUint(fields[5], 10, 64)
		writes, _ := strconv.ParseUint(fields[7], 10, 64)
		sectorsWritten, _ := strconv.ParseUint(fields[9], 10, 64)
		ioTicks, _ := strconv.ParseUint(fields[12], 10, 64) // ms spent doing I/O

		cur := rawIOStat{
			reads:      reads,
			readBytes:  sectorsRead * 512,
			writes:     writes,
			writeBytes: sectorsWritten * 512,
			ioTicks:    ioTicks,
			ts:         now,
		}

		if prev, ok := prevIOStats[name]; ok {
			elapsed := cur.ts.Sub(prev.ts).Seconds()
			if elapsed > 0 {
				io := IOStats{Device: name}
				io.ReadsPerSec = float64(cur.reads-prev.reads) / elapsed
				io.WritesPerSec = float64(cur.writes-prev.writes) / elapsed
				io.ReadBytesPerSec = float64(cur.readBytes-prev.readBytes) / elapsed
				io.WriteBytesPerSec = float64(cur.writeBytes-prev.writeBytes) / elapsed
				tickDelta := float64(cur.ioTicks - prev.ioTicks)
				// UtilPct: ms spent in I/O / (elapsed * 1000ms)
				io.UtilPct = tickDelta / (elapsed * 1000) * 100
				if io.UtilPct > 100 {
					io.UtilPct = 100
				}
				result = append(result, io)
			}
		}

		prevIOStats[name] = cur
	}

	return result, scanner.Err()
}

func isPartition(name string) bool {
	// nvme0n1p1 → is a partition; nvme0n1 → is not
	// sda1 → partition; sda → not
	if len(name) == 0 {
		return false
	}
	last := name[len(name)-1]
	if last >= '0' && last <= '9' {
		// Check if it's an NVMe disk (nvme0n1) vs partition (nvme0n1p1)
		if strings.Contains(name, "nvme") && !strings.Contains(name, "p") {
			return false
		}
		return true
	}
	return false
}
