package gcp

import (
	"context"
	"fmt"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/errors"
	"github.com/afterdarksys/opskit/cloudtop/internal/metrics"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
	"github.com/afterdarksys/opskit/cloudtop/pkg/ratelimit"
)

func init() {
	provider.Register("gcp", func() provider.Provider {
		return &GCPProvider{}
	})
}

// GCPProvider implements Provider interface for GCP (stub)
type GCPProvider struct {
	config    *provider.ProviderConfig
	projectID string
	limiter   *ratelimit.Limiter
}

func (p *GCPProvider) Name() string {
	return "gcp"
}

func (p *GCPProvider) Initialize(ctx context.Context, config *provider.ProviderConfig) error {
	p.config = config

	if projID, ok := config.Options["project_id"].(string); ok {
		p.projectID = projID
	} else {
		return errors.NewValidationError("gcp", "project_id required")
	}

	if config.RateLimit != nil {
		p.limiter = ratelimit.NewLimiter(
			config.RateLimit.RequestsPerSecond,
			config.RateLimit.Burst,
			config.RateLimit.Timeout,
		)
	} else {
		p.limiter = ratelimit.NewLimiter(10, 20, 30*time.Second)
	}

	return nil
}

func (p *GCPProvider) HealthCheck(ctx context.Context) error {
	// Stub - would verify GCP credentials
	return fmt.Errorf("gcp provider not yet implemented")
}

func (p *GCPProvider) ListServices(ctx context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "compute", Name: "Compute Engine", Type: "compute", Capabilities: []string{"compute", "metrics", "gpu"}},
		{ID: "gke", Name: "Google Kubernetes Engine", Type: "containers", Capabilities: []string{"containers", "kubernetes"}},
		{ID: "functions", Name: "Cloud Functions", Type: "serverless", Capabilities: []string{"compute", "metrics"}},
		{ID: "gcs", Name: "Cloud Storage", Type: "storage", Capabilities: []string{"storage", "metrics"}},
		{ID: "cloudsql", Name: "Cloud SQL", Type: "database", Capabilities: []string{"database", "metrics"}},
	}, nil
}

func (p *GCPProvider) ListResources(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Resource, error) {
	return nil, fmt.Errorf("gcp provider not yet implemented")
}

func (p *GCPProvider) GetMetrics(ctx context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	return &provider.MetricsResponse{
		Provider:  "gcp",
		Metrics:   make(map[string]interface{}),
		Timestamp: time.Now(),
		Cached:    false,
	}, nil
}

func (p *GCPProvider) Close() error {
	return nil
}

// ComputeProvider interface
func (p *GCPProvider) ListInstances(ctx context.Context, filter *provider.InstanceFilter) ([]provider.Instance, error) {
	return nil, fmt.Errorf("gcp provider not yet implemented")
}

func (p *GCPProvider) GetInstanceMetrics(ctx context.Context, instanceID string) (*metrics.ComputeMetrics, error) {
	return nil, fmt.Errorf("gcp provider not yet implemented")
}

// GPUProvider interface
func (p *GCPProvider) ListGPUInstances(ctx context.Context, filter *provider.GPUFilter) ([]provider.GPUInstance, error) {
	return nil, fmt.Errorf("gcp provider not yet implemented")
}

func (p *GCPProvider) GetGPUMetrics(ctx context.Context, instanceID string) (*metrics.GPUMetrics, error) {
	return nil, fmt.Errorf("gcp provider not yet implemented")
}

func (p *GCPProvider) GetGPUAvailability(ctx context.Context) ([]provider.GPUOffering, error) {
	return nil, fmt.Errorf("gcp provider not yet implemented")
}
