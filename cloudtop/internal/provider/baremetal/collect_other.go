//go:build !linux

package baremetal

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// fillMetrics collects what's available on non-Linux platforms.
// On macOS this covers memory (via sysctl) and disk (via statfs).
// CPU usage sampling via /proc is not available; a best-effort
// stub is returned so the provider still functions for testing.
func fillMetrics(h *HostMetrics) error {
	fillMemoryOther(h)
	fillDisksOther(h)
	return nil
}

func fillMemoryOther(h *HostMetrics) {
	// hw.memsize is a 64-bit integer on macOS; use unix.SysctlRaw.
	b, err := unix.SysctlRaw("hw.memsize")
	if err != nil || len(b) < 8 {
		return
	}
	total := int64(binary.LittleEndian.Uint64(b[:8]))
	h.Memory = MemoryStats{
		TotalBytes: total,
	}
}

func fillDisksOther(h *HostMetrics) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return
	}

	bsize := int64(stat.Bsize)
	total := int64(stat.Blocks) * bsize
	free := int64(stat.Bfree) * bsize
	used := total - free
	var pct float64
	if total > 0 {
		pct = float64(used) / float64(total) * 100.0
	}

	h.Disks = []DiskStats{{
		MountPoint:   "/",
		TotalBytes:   total,
		UsedBytes:    used,
		FreeBytes:    free,
		UsagePercent: pct,
		FSType:       fmt.Sprintf("%s/root", runtime.GOOS),
	}}
}
