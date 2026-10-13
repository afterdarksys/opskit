package provider

import (
	"context"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/metrics"
)

// Provider is the core interface that all cloud providers must implement
type Provider interface {
	// Name returns the provider identifier (e.g., "cloudflare", "oracle")
	Name() string

	// Initialize sets up the provider with credentials and config
	Initialize(ctx context.Context, config *ProviderConfig) error

	// HealthCheck verifies the provider is accessible
	HealthCheck(ctx context.Context) error

	// ListServices returns available services for this provider
	ListServices(ctx context.Context) ([]Service, error)

	// GetMetrics retrieves metrics for specified resources
	GetMetrics(ctx context.Context, req *MetricsRequest) (*MetricsResponse, error)

	// ListResources lists all resources (VMs, containers, functions)
	ListResources(ctx context.Context, filter *ResourceFilter) ([]Resource, error)

	// Close cleans up resources
	Close() error
}

// ComputeProvider extends Provider with compute-specific capabilities
type ComputeProvider interface {
	Provider

	// ListInstances returns compute instances (VMs, containers)
	ListInstances(ctx context.Context, filter *InstanceFilter) ([]Instance, error)

	// GetInstanceMetrics retrieves metrics for specific instance
	GetInstanceMetrics(ctx context.Context, instanceID string) (*metrics.ComputeMetrics, error)
}

// GPUProvider extends Provider with GPU-specific capabilities
type GPUProvider interface {
	Provider

	// ListGPUInstances returns GPU-enabled instances
	ListGPUInstances(ctx context.Context, filter *GPUFilter) ([]GPUInstance, error)

	// GetGPUMetrics retrieves GPU utilization metrics
	GetGPUMetrics(ctx context.Context, instanceID string) (*metrics.GPUMetrics, error)

	// GetGPUAvailability returns available GPU types and pricing
	GetGPUAvailability(ctx context.Context) ([]GPUOffering, error)
}

// ServerlessProvider extends Provider with serverless capabilities
type ServerlessProvider interface {
	Provider

	// ListFunctions returns serverless functions
	ListFunctions(ctx context.Context, filter *FunctionFilter) ([]Function, error)

	// GetFunctionMetrics retrieves function execution metrics
	GetFunctionMetrics(ctx context.Context, functionID string) (*metrics.FunctionMetrics, error)
}

// StorageProvider extends Provider with storage capabilities
type StorageProvider interface {
	Provider

	// ListBuckets returns storage buckets
	ListBuckets(ctx context.Context) ([]Bucket, error)

	// GetStorageMetrics retrieves storage usage metrics
	GetStorageMetrics(ctx context.Context, bucketID string) (*metrics.StorageMetrics, error)
}

// DatabaseProvider extends Provider with database capabilities
type DatabaseProvider interface {
	Provider

	// ListDatabases returns database instances
	ListDatabases(ctx context.Context) ([]Database, error)

	// GetDatabaseMetrics retrieves database metrics
	GetDatabaseMetrics(ctx context.Context, dbID string) (*metrics.DatabaseMetrics, error)
}

// AIProvider extends Provider with AI/inference capabilities
type AIProvider interface {
	Provider

	// ListModels returns available AI models
	ListModels(ctx context.Context) ([]AIModel, error)

	// GetAIMetrics retrieves AI workload metrics
	GetAIMetrics(ctx context.Context, resourceID string) (*metrics.AIMetrics, error)
}

// ProviderConfig holds provider-specific configuration
type ProviderConfig struct {
	Name        string                 `json:"name"`
	Enabled     bool                   `json:"enabled"`
	Credentials map[string]string      `json:"credentials"`
	Options     map[string]interface{} `json:"options"`
	RateLimit   *RateLimitConfig       `json:"rate_limit,omitempty"`
	Cache       *CacheConfig           `json:"cache,omitempty"`
}

// RateLimitConfig defines rate limiting parameters
type RateLimitConfig struct {
	RequestsPerSecond float64       `json:"requests_per_second"`
	Burst             int           `json:"burst"`
	Timeout           time.Duration `json:"timeout"`
}

// CacheConfig defines caching parameters
type CacheConfig struct {
	Enabled bool          `json:"enabled"`
	TTL     time.Duration `json:"ttl"`
	MaxSize int           `json:"max_size"`
}

// Service represents a cloud service
type Service struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Capabilities []string `json:"capabilities"`
}

// Resource is a generic cloud resource
type Resource struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Provider  string            `json:"provider"`
	Region    string            `json:"region"`
	Status    string            `json:"status"`
	Tags      map[string]string `json:"tags"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`

	// Cost tracking
	HourlyRate float64 `json:"hourly_rate,omitempty"`
}

// Instance represents a compute instance
type Instance struct {
	Resource
	InstanceType string  `json:"instance_type"`
	PublicIP     string  `json:"public_ip,omitempty"`
	PrivateIP    string  `json:"private_ip,omitempty"`
	CPUCores     int     `json:"cpu_cores"`
	MemoryGB     float64 `json:"memory_gb"`
	State        string  `json:"state"`
}

// GPUInstance represents a GPU-enabled instance
type GPUInstance struct {
	Instance
	GPUType      string  `json:"gpu_type"`
	GPUCount     int     `json:"gpu_count"`
	GPUMemoryGB  float64 `json:"gpu_memory_gb"`
	PricePerHour float64 `json:"price_per_hour,omitempty"`
}

// GPUOffering represents available GPU instance types
type GPUOffering struct {
	Provider     string  `json:"provider"`
	GPUType      string  `json:"gpu_type"`
	GPUCount     int     `json:"gpu_count"`
	GPUMemoryGB  float64 `json:"gpu_memory_gb"`
	CPUCores     int     `json:"cpu_cores"`
	MemoryGB     float64 `json:"memory_gb"`
	DiskGB       float64 `json:"disk_gb,omitempty"`
	PricePerHour float64 `json:"price_per_hour"`
	Available    bool    `json:"available"`
	Region       string  `json:"region"`
	InstanceType string  `json:"instance_type,omitempty"`
}

// Function represents a serverless function
type Function struct {
	Resource
	Runtime      string    `json:"runtime"`
	MemoryMB     int       `json:"memory_mb"`
	Timeout      int       `json:"timeout"`
	Handler      string    `json:"handler,omitempty"`
	LastModified time.Time `json:"last_modified"`
}

// Bucket represents a storage bucket
type Bucket struct {
	Resource
	SizeBytes    int64  `json:"size_bytes"`
	ObjectCount  int64  `json:"object_count"`
	StorageClass string `json:"storage_class"`
}

// Database represents a database instance
type Database struct {
	Resource
	Engine      string  `json:"engine"`
	Version     string  `json:"version"`
	SizeGB      float64 `json:"size_gb"`
	Connections int     `json:"connections"`
	Endpoint    string  `json:"endpoint,omitempty"`
}

// AIModel represents an AI model
type AIModel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`
	Type        string  `json:"type"` // "llm", "embedding", "image", etc.
	MaxTokens   int     `json:"max_tokens,omitempty"`
	PricePerReq float64 `json:"price_per_request,omitempty"`
}

// MetricsRequest specifies what metrics to collect
type MetricsRequest struct {
	ResourceIDs []string          `json:"resource_ids"`
	MetricNames []string          `json:"metric_names"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Granularity time.Duration     `json:"granularity"`
	Filters     map[string]string `json:"filters"`
}

// MetricsResponse contains collected metrics
type MetricsResponse struct {
	Provider  string                 `json:"provider"`
	Metrics   map[string]interface{} `json:"metrics"`
	Timestamp time.Time              `json:"timestamp"`
	Cached    bool                   `json:"cached"`
}

// ResourceFilter for filtering resources
type ResourceFilter struct {
	Types       []string          `json:"types,omitempty"`
	Regions     []string          `json:"regions,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Status      []string          `json:"status,omitempty"`
	NamePattern string            `json:"name_pattern,omitempty"`
}

// InstanceFilter for filtering instances
type InstanceFilter struct {
	ResourceFilter
	States        []string `json:"states,omitempty"`
	InstanceTypes []string `json:"instance_types,omitempty"`
}

// GPUFilter for filtering GPU instances
type GPUFilter struct {
	InstanceFilter
	GPUTypes     []string `json:"gpu_types,omitempty"`
	MinGPUMemory float64  `json:"min_gpu_memory,omitempty"`
	MaxPrice     float64  `json:"max_price,omitempty"`
}

// FunctionFilter for filtering functions
type FunctionFilter struct {
	ResourceFilter
	Runtimes []string `json:"runtimes,omitempty"`
}
