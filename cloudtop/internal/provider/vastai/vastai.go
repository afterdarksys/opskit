package vastai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/errors"
	"github.com/afterdarksys/opskit/cloudtop/internal/metrics"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
	"github.com/afterdarksys/opskit/cloudtop/pkg/ratelimit"
)

func init() {
	provider.Register("vastai", func() provider.Provider {
		return &VastAIProvider{}
	})
}

// VastAIProvider implements Provider and GPUProvider interfaces
type VastAIProvider struct {
	config  *provider.ProviderConfig
	apiKey  string
	client  *http.Client
	limiter *ratelimit.Limiter
}

const baseURL = "https://console.vast.ai/api/v0"

func (p *VastAIProvider) Name() string {
	return "vastai"
}

func (p *VastAIProvider) Initialize(ctx context.Context, config *provider.ProviderConfig) error {
	p.config = config

	// Get API key from credentials
	apiKey, ok := config.Credentials["api_token"]
	if !ok || apiKey == "" {
		return errors.NewAuthError("vastai", fmt.Errorf("missing api_token (VASTAI_API_KEY)"))
	}
	p.apiKey = apiKey

	// Create HTTP client
	p.client = &http.Client{
		Timeout: 30 * time.Second,
	}

	// Set up rate limiter
	if config.RateLimit != nil {
		p.limiter = ratelimit.NewLimiter(
			config.RateLimit.RequestsPerSecond,
			config.RateLimit.Burst,
			config.RateLimit.Timeout,
		)
	} else {
		// Default rate limit for Vast.ai API
		p.limiter = ratelimit.NewLimiter(5, 10, 30*time.Second)
	}

	return nil
}

func (p *VastAIProvider) HealthCheck(ctx context.Context) error {
	if err := p.limiter.Wait(ctx); err != nil {
		return errors.NewRateLimitError("vastai", err)
	}

	// Verify by checking user info
	_, err := p.doRequest(ctx, "GET", "/users/current")
	return err
}

func (p *VastAIProvider) ListServices(ctx context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "instances", Name: "GPU Instances", Type: "compute", Capabilities: []string{"gpu", "compute", "metrics"}},
		{ID: "marketplace", Name: "GPU Marketplace", Type: "marketplace", Capabilities: []string{"gpu", "pricing"}},
	}, nil
}

func (p *VastAIProvider) ListResources(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Resource, error) {
	instances, err := p.listInstances(ctx)
	if err != nil {
		return nil, err
	}

	var resources []provider.Resource
	for _, inst := range instances {
		status := "running"
		if !inst.IsRunning {
			status = "stopped"
		}

		resources = append(resources, provider.Resource{
			ID:       fmt.Sprintf("%d", inst.ID),
			Name:     inst.Label,
			Type:     "gpu_instance",
			Provider: "vastai",
			Region:   inst.Geolocation,
			Status:   status,
		})
	}

	return resources, nil
}

func (p *VastAIProvider) GetMetrics(ctx context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	return &provider.MetricsResponse{
		Provider:  "vastai",
		Metrics:   make(map[string]interface{}),
		Timestamp: time.Now(),
		Cached:    false,
	}, nil
}

func (p *VastAIProvider) Close() error {
	return nil
}

// GPUProvider interface
func (p *VastAIProvider) ListGPUInstances(ctx context.Context, filter *provider.GPUFilter) ([]provider.GPUInstance, error) {
	instances, err := p.listInstances(ctx)
	if err != nil {
		return nil, err
	}

	var gpuInstances []provider.GPUInstance
	for _, inst := range instances {
		status := "running"
		if !inst.IsRunning {
			status = "stopped"
		}

		gpuInstance := provider.GPUInstance{
			Instance: provider.Instance{
				Resource: provider.Resource{
					ID:       fmt.Sprintf("%d", inst.ID),
					Name:     inst.Label,
					Type:     "gpu_instance",
					Provider: "vastai",
					Region:   inst.Geolocation,
					Status:   status,
				},
				InstanceType: inst.GPUName,
				CPUCores:     inst.CPUCores,
				MemoryGB:     float64(inst.RAMTotalMB) / 1024,
				State:        status,
			},
			GPUType:      inst.GPUName,
			GPUCount:     inst.NumGPUs,
			GPUMemoryGB:  float64(inst.GPUTotalRAM) / 1024,
			PricePerHour: inst.DPHTotal,
		}

		// Apply filters
		if filter != nil {
			if len(filter.GPUTypes) > 0 && !contains(filter.GPUTypes, inst.GPUName) {
				continue
			}
			if filter.MaxPrice > 0 && inst.DPHTotal > filter.MaxPrice {
				continue
			}
		}

		gpuInstances = append(gpuInstances, gpuInstance)
	}

	return gpuInstances, nil
}

func (p *VastAIProvider) GetGPUMetrics(ctx context.Context, instanceID string) (*metrics.GPUMetrics, error) {
	return &metrics.GPUMetrics{
		ResourceID: instanceID,
		Provider:   "vastai",
		Timestamp:  time.Now(),
		GPUs:       []metrics.GPUDeviceMetrics{},
	}, nil
}

func (p *VastAIProvider) GetGPUAvailability(ctx context.Context) ([]provider.GPUOffering, error) {
	offers, err := p.searchOffers(ctx)
	if err != nil {
		return nil, err
	}

	var offerings []provider.GPUOffering
	seen := make(map[string]bool)

	for _, offer := range offers {
		// Dedupe by GPU type + count
		key := fmt.Sprintf("%s-%d", offer.GPUName, offer.NumGPUs)
		if seen[key] {
			continue
		}
		seen[key] = true

		offerings = append(offerings, provider.GPUOffering{
			Provider:     "vastai",
			GPUType:      offer.GPUName,
			GPUCount:     offer.NumGPUs,
			GPUMemoryGB:  float64(offer.GPUTotalRAM) / 1024,
			CPUCores:     offer.CPUCores,
			MemoryGB:     float64(offer.RAMTotalMB) / 1024,
			DiskGB:       float64(offer.DiskSpace),
			PricePerHour: offer.DPHTotal,
			Available:    offer.Rentable,
			Region:       offer.Geolocation,
		})
	}

	return offerings, nil
}

// API types
type vastInstance struct {
	ID          int     `json:"id"`
	Label       string  `json:"label"`
	GPUName     string  `json:"gpu_name"`
	NumGPUs     int     `json:"num_gpus"`
	GPUTotalRAM int     `json:"gpu_total_ram"`
	CPUCores    int     `json:"cpu_cores"`
	RAMTotalMB  int     `json:"cpu_ram"`
	DiskSpace   float64 `json:"disk_space"`
	DPHTotal    float64 `json:"dph_total"`
	IsRunning   bool    `json:"actual_status"`
	Geolocation string  `json:"geolocation"`
}

type vastOffer struct {
	ID          int     `json:"id"`
	GPUName     string  `json:"gpu_name"`
	NumGPUs     int     `json:"num_gpus"`
	GPUTotalRAM int     `json:"gpu_total_ram"`
	CPUCores    int     `json:"cpu_cores"`
	RAMTotalMB  int     `json:"cpu_ram"`
	DiskSpace   float64 `json:"disk_space"`
	DPHTotal    float64 `json:"dph_total"`
	Rentable    bool    `json:"rentable"`
	Geolocation string  `json:"geolocation"`
}

func (p *VastAIProvider) doRequest(ctx context.Context, method, path string) ([]byte, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, errors.NewRateLimitError("vastai", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
	if err != nil {
		return nil, errors.NewInternalError("vastai", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errors.NewNetworkError("vastai", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewNetworkError("vastai", err)
	}

	if resp.StatusCode >= 400 {
		return nil, errors.NewNetworkError("vastai", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body)))
	}

	return body, nil
}

func (p *VastAIProvider) listInstances(ctx context.Context) ([]vastInstance, error) {
	body, err := p.doRequest(ctx, "GET", "/instances")
	if err != nil {
		return nil, err
	}

	var result struct {
		Instances []vastInstance `json:"instances"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.NewInternalError("vastai", err)
	}

	return result.Instances, nil
}

func (p *VastAIProvider) searchOffers(ctx context.Context) ([]vastOffer, error) {
	body, err := p.doRequest(ctx, "GET", "/bundles?q={\"rentable\":true}")
	if err != nil {
		return nil, err
	}

	var result struct {
		Offers []vastOffer `json:"offers"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.NewInternalError("vastai", err)
	}

	return result.Offers, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
