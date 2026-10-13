package azure

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
	provider.Register("azure", func() provider.Provider {
		return &AzureProvider{}
	})
}

// AzureProvider implements Provider interface for Azure (stub)
type AzureProvider struct {
	config         *provider.ProviderConfig
	subscriptionID string
	limiter        *ratelimit.Limiter
}

func (p *AzureProvider) Name() string {
	return "azure"
}

func (p *AzureProvider) Initialize(ctx context.Context, config *provider.ProviderConfig) error {
	p.config = config

	if subID, ok := config.Options["subscription_id"].(string); ok {
		p.subscriptionID = subID
	} else {
		return errors.NewValidationError("azure", "subscription_id required")
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

func (p *AzureProvider) HealthCheck(ctx context.Context) error {
	// Stub - would verify Azure credentials
	return fmt.Errorf("azure provider not yet implemented")
}

func (p *AzureProvider) ListServices(ctx context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "vms", Name: "Virtual Machines", Type: "compute", Capabilities: []string{"compute", "metrics"}},
		{ID: "aks", Name: "Azure Kubernetes Service", Type: "containers", Capabilities: []string{"containers", "kubernetes"}},
		{ID: "functions", Name: "Azure Functions", Type: "serverless", Capabilities: []string{"compute", "metrics"}},
		{ID: "storage", Name: "Blob Storage", Type: "storage", Capabilities: []string{"storage", "metrics"}},
		{ID: "sql", Name: "Azure SQL", Type: "database", Capabilities: []string{"database", "metrics"}},
	}, nil
}

func (p *AzureProvider) ListResources(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Resource, error) {
	return nil, fmt.Errorf("azure provider not yet implemented")
}

func (p *AzureProvider) GetMetrics(ctx context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	return &provider.MetricsResponse{
		Provider:  "azure",
		Metrics:   make(map[string]interface{}),
		Timestamp: time.Now(),
		Cached:    false,
	}, nil
}

func (p *AzureProvider) Close() error {
	return nil
}

// ComputeProvider interface
func (p *AzureProvider) ListInstances(ctx context.Context, filter *provider.InstanceFilter) ([]provider.Instance, error) {
	return nil, fmt.Errorf("azure provider not yet implemented")
}

func (p *AzureProvider) GetInstanceMetrics(ctx context.Context, instanceID string) (*metrics.ComputeMetrics, error) {
	return nil, fmt.Errorf("azure provider not yet implemented")
}
