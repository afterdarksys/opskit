package metrics

import (
	"time"
)

// ComputeMetrics represents compute instance metrics
type ComputeMetrics struct {
	ResourceID string    `json:"resource_id"`
	Provider   string    `json:"provider"`
	Timestamp  time.Time `json:"timestamp"`

	// CPU Metrics
	CPUUsagePercent     float64 `json:"cpu_usage_percent"`
	CPUCores            int     `json:"cpu_cores"`
	CPUCreditsRemaining float64 `json:"cpu_credits_remaining,omitempty"`

	// Memory Metrics
	MemoryUsedBytes    int64   `json:"memory_used_bytes"`
	MemoryTotalBytes   int64   `json:"memory_total_bytes"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	SwapUsedBytes      int64   `json:"swap_used_bytes,omitempty"`

	// Disk I/O Metrics
	DiskReadBytesPerSec  int64   `json:"disk_read_bytes_per_sec"`
	DiskWriteBytesPerSec int64   `json:"disk_write_bytes_per_sec"`
	DiskReadOpsPerSec    float64 `json:"disk_read_ops_per_sec"`
	DiskWriteOpsPerSec   float64 `json:"disk_write_ops_per_sec"`

	// Network Metrics
	NetworkInBytesPerSec   int64   `json:"network_in_bytes_per_sec"`
	NetworkOutBytesPerSec  int64   `json:"network_out_bytes_per_sec"`
	NetworkInPacketsPerSec float64 `json:"network_in_packets_per_sec"`
	NetworkOutPacketsPerSec float64 `json:"network_out_packets_per_sec"`
}

// GPUMetrics represents GPU-specific metrics
type GPUMetrics struct {
	ResourceID string            `json:"resource_id"`
	Provider   string            `json:"provider"`
	Timestamp  time.Time         `json:"timestamp"`
	GPUs       []GPUDeviceMetrics `json:"gpus"`
}

// GPUDeviceMetrics represents metrics for a single GPU
type GPUDeviceMetrics struct {
	DeviceID           int     `json:"device_id"`
	Name               string  `json:"name"`
	GPUUtilization     float64 `json:"gpu_utilization_percent"`
	MemoryUtilization  float64 `json:"memory_utilization_percent"`
	MemoryUsedBytes    int64   `json:"memory_used_bytes"`
	MemoryTotalBytes   int64   `json:"memory_total_bytes"`
	TemperatureCelsius float64 `json:"temperature_celsius"`
	PowerUsageWatts    float64 `json:"power_usage_watts"`
	PowerLimitWatts    float64 `json:"power_limit_watts"`
	ClockSpeedMHz      int     `json:"clock_speed_mhz"`
	MemoryClockMHz     int     `json:"memory_clock_mhz"`
}

// FunctionMetrics represents serverless function metrics
type FunctionMetrics struct {
	ResourceID      string    `json:"resource_id"`
	Provider        string    `json:"provider"`
	Timestamp       time.Time `json:"timestamp"`
	InvocationCount int64     `json:"invocation_count"`
	ErrorCount      int64     `json:"error_count"`
	ThrottleCount   int64     `json:"throttle_count"`
	AvgDuration     float64   `json:"avg_duration_ms"`
	MaxDuration     float64   `json:"max_duration_ms"`
	MinDuration     float64   `json:"min_duration_ms"`
	AvgMemoryUsedMB float64   `json:"avg_memory_used_mb"`
	ConcurrentExecs int       `json:"concurrent_executions"`
}

// StorageMetrics represents storage metrics
type StorageMetrics struct {
	ResourceID      string    `json:"resource_id"`
	Provider        string    `json:"provider"`
	Timestamp       time.Time `json:"timestamp"`
	TotalSizeBytes  int64     `json:"total_size_bytes"`
	ObjectCount     int64     `json:"object_count"`
	GetRequests     int64     `json:"get_requests"`
	PutRequests     int64     `json:"put_requests"`
	DeleteRequests  int64     `json:"delete_requests"`
	ListRequests    int64     `json:"list_requests"`
	BytesDownloaded int64     `json:"bytes_downloaded"`
	BytesUploaded   int64     `json:"bytes_uploaded"`
}

// DatabaseMetrics represents database metrics
type DatabaseMetrics struct {
	ResourceID         string    `json:"resource_id"`
	Provider           string    `json:"provider"`
	Timestamp          time.Time `json:"timestamp"`
	ActiveConnections  int       `json:"active_connections"`
	MaxConnections     int       `json:"max_connections"`
	IdleConnections    int       `json:"idle_connections"`
	QueriesPerSecond   float64   `json:"queries_per_second"`
	AvgQueryDurationMs float64   `json:"avg_query_duration_ms"`
	SlowQueries        int64     `json:"slow_queries"`
	DatabaseSizeBytes  int64     `json:"database_size_bytes"`
	CacheHitRatio      float64   `json:"cache_hit_ratio,omitempty"`
	ReplicationLagMs   float64   `json:"replication_lag_ms,omitempty"`
}

// AIMetrics represents AI/inference workload metrics
type AIMetrics struct {
	ResourceID         string    `json:"resource_id"`
	Provider           string    `json:"provider"`
	Timestamp          time.Time `json:"timestamp"`
	InferenceCount     int64     `json:"inference_count"`
	TokensProcessed    int64     `json:"tokens_processed"`
	AvgLatencyMs       float64   `json:"avg_latency_ms"`
	P99LatencyMs       float64   `json:"p99_latency_ms"`
	ErrorRate          float64   `json:"error_rate"`
	QueueDepth         int       `json:"queue_depth"`
	CostPerHour        float64   `json:"cost_per_hour"`
}

// FormatBytes formats bytes to human readable string
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return formatValue(float64(bytes), "B")
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return formatValue(float64(bytes)/float64(div), "KMGTPE"[exp:exp+1]+"B")
}

// FormatPercent formats percentage
func FormatPercent(pct float64) string {
	return formatValue(pct, "%")
}

func formatValue(val float64, suffix string) string {
	if val < 10 {
		return sprintf("%.1f%s", val, suffix)
	}
	return sprintf("%.0f%s", val, suffix)
}

func sprintf(format string, args ...interface{}) string {
	result := format
	for _, arg := range args {
		switch v := arg.(type) {
		case float64:
			if result[0:4] == "%.1f" {
				result = floatToString(v, 1) + result[4:]
			} else if result[0:4] == "%.0f" {
				result = floatToString(v, 0) + result[4:]
			}
		case string:
			result = result[:len(result)-2] + v
		}
	}
	return result
}

func floatToString(f float64, precision int) string {
	if precision == 0 {
		return intToString(int64(f))
	}
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return intToString(whole) + "." + intToString(frac)
}

func intToString(i int64) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var result []byte
	for i > 0 {
		result = append([]byte{byte('0' + i%10)}, result...)
		i /= 10
	}
	if negative {
		result = append([]byte{'-'}, result...)
	}
	return string(result)
}
