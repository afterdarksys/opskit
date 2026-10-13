package runpod

import (
	"bytes"
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
	provider.Register("runpod", func() provider.Provider {
		return &RunPodProvider{}
	})
}

// RunPodProvider implements Provider and GPUProvider interfaces
type RunPodProvider struct {
	config  *provider.ProviderConfig
	apiKey  string
	client  *http.Client
	limiter *ratelimit.Limiter
}

const graphqlURL = "https://api.runpod.io/graphql"

func (p *RunPodProvider) Name() string {
	return "runpod"
}

func (p *RunPodProvider) Initialize(ctx context.Context, config *provider.ProviderConfig) error {
	p.config = config

	// Get API key from credentials
	apiKey, ok := config.Credentials["api_token"]
	if !ok || apiKey == "" {
		return errors.NewAuthError("runpod", fmt.Errorf("missing api_token (RUNPOD_API_KEY)"))
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
		// Default rate limit
		p.limiter = ratelimit.NewLimiter(10, 20, 30*time.Second)
	}

	return nil
}

func (p *RunPodProvider) HealthCheck(ctx context.Context) error {
	if err := p.limiter.Wait(ctx); err != nil {
		return errors.NewRateLimitError("runpod", err)
	}

	// Verify by querying user info
	query := `query { myself { id email } }`
	_, err := p.doGraphQL(ctx, query, nil)
	return err
}

func (p *RunPodProvider) ListServices(ctx context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "pods", Name: "GPU Pods", Type: "compute", Capabilities: []string{"gpu", "compute", "metrics"}},
		{ID: "serverless", Name: "Serverless GPU", Type: "serverless", Capabilities: []string{"gpu", "inference"}},
		{ID: "templates", Name: "Pod Templates", Type: "templates", Capabilities: []string{"templates"}},
	}, nil
}

func (p *RunPodProvider) ListResources(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Resource, error) {
	pods, err := p.listPods(ctx)
	if err != nil {
		return nil, err
	}

	var resources []provider.Resource
	for _, pod := range pods {
		resources = append(resources, provider.Resource{
			ID:       pod.ID,
			Name:     pod.Name,
			Type:     "gpu_pod",
			Provider: "runpod",
			Region:   pod.DataCenter,
			Status:   pod.DesiredStatus,
		})
	}

	return resources, nil
}

func (p *RunPodProvider) GetMetrics(ctx context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	return &provider.MetricsResponse{
		Provider:  "runpod",
		Metrics:   make(map[string]interface{}),
		Timestamp: time.Now(),
		Cached:    false,
	}, nil
}

func (p *RunPodProvider) Close() error {
	return nil
}

// GPUProvider interface
func (p *RunPodProvider) ListGPUInstances(ctx context.Context, filter *provider.GPUFilter) ([]provider.GPUInstance, error) {
	pods, err := p.listPods(ctx)
	if err != nil {
		return nil, err
	}

	var gpuInstances []provider.GPUInstance
	for _, pod := range pods {
		gpuInstance := provider.GPUInstance{
			Instance: provider.Instance{
				Resource: provider.Resource{
					ID:       pod.ID,
					Name:     pod.Name,
					Type:     "gpu_pod",
					Provider: "runpod",
					Region:   pod.DataCenter,
					Status:   pod.DesiredStatus,
				},
				InstanceType: pod.GPUType,
				CPUCores:     pod.VcpuCount,
				MemoryGB:     float64(pod.MemoryInGb),
				State:        pod.DesiredStatus,
			},
			GPUType:      pod.GPUType,
			GPUCount:     pod.GPUCount,
			GPUMemoryGB:  float64(pod.GPUMemoryGb),
			PricePerHour: pod.CostPerHr,
		}

		// Apply filters
		if filter != nil {
			if len(filter.GPUTypes) > 0 && !contains(filter.GPUTypes, pod.GPUType) {
				continue
			}
			if filter.MaxPrice > 0 && pod.CostPerHr > filter.MaxPrice {
				continue
			}
		}

		gpuInstances = append(gpuInstances, gpuInstance)
	}

	return gpuInstances, nil
}

func (p *RunPodProvider) GetGPUMetrics(ctx context.Context, instanceID string) (*metrics.GPUMetrics, error) {
	return &metrics.GPUMetrics{
		ResourceID: instanceID,
		Provider:   "runpod",
		Timestamp:  time.Now(),
		GPUs:       []metrics.GPUDeviceMetrics{},
	}, nil
}

func (p *RunPodProvider) GetGPUAvailability(ctx context.Context) ([]provider.GPUOffering, error) {
	types, err := p.listGPUTypes(ctx)
	if err != nil {
		return nil, err
	}

	var offerings []provider.GPUOffering
	for _, t := range types {
		offerings = append(offerings, provider.GPUOffering{
			Provider:     "runpod",
			GPUType:      t.DisplayName,
			GPUCount:     1,
			GPUMemoryGB:  float64(t.MemoryInGb),
			CPUCores:     0, // Varies by configuration
			MemoryGB:     0,
			PricePerHour: t.LowestPrice.MinimumBidPrice,
			Available:    t.LowestPrice.StockStatus == "high" || t.LowestPrice.StockStatus == "medium",
			Region:       "global",
			InstanceType: t.ID,
		})
	}

	return offerings, nil
}

// API types
type runpodPod struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	GPUType       string  `json:"gpuTypeId"`
	GPUCount      int     `json:"gpuCount"`
	GPUMemoryGb   int     `json:"gpuMemoryGb"`
	VcpuCount     int     `json:"vcpuCount"`
	MemoryInGb    int     `json:"memoryInGb"`
	CostPerHr     float64 `json:"costPerHr"`
	DesiredStatus string  `json:"desiredStatus"`
	DataCenter    string  `json:"dataCenterId"`
}

type runpodGPUType struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	MemoryInGb  int    `json:"memoryInGb"`
	LowestPrice struct {
		MinimumBidPrice float64 `json:"minimumBidPrice"`
		StockStatus     string  `json:"stockStatus"`
	} `json:"lowestPrice"`
}

type graphqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

func (p *RunPodProvider) doGraphQL(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, errors.NewRateLimitError("runpod", err)
	}

	reqBody := graphqlRequest{
		Query:     query,
		Variables: variables,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, errors.NewInternalError("runpod", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", graphqlURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, errors.NewInternalError("runpod", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errors.NewNetworkError("runpod", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewNetworkError("runpod", err)
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, errors.NewInternalError("runpod", err)
	}

	if len(gqlResp.Errors) > 0 {
		return nil, errors.NewNetworkError("runpod", fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message))
	}

	return gqlResp.Data, nil
}

func (p *RunPodProvider) listPods(ctx context.Context) ([]runpodPod, error) {
	query := `
		query {
			myself {
				pods {
					id
					name
					gpuTypeId
					gpuCount
					vcpuCount
					memoryInGb
					costPerHr
					desiredStatus
					dataCenterId
				}
			}
		}
	`

	data, err := p.doGraphQL(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Myself struct {
			Pods []runpodPod `json:"pods"`
		} `json:"myself"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.NewInternalError("runpod", err)
	}

	return result.Myself.Pods, nil
}

func (p *RunPodProvider) listGPUTypes(ctx context.Context) ([]runpodGPUType, error) {
	query := `
		query {
			gpuTypes {
				id
				displayName
				memoryInGb
				lowestPrice(gpuCount: 1) {
					minimumBidPrice
					stockStatus
				}
			}
		}
	`

	data, err := p.doGraphQL(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		GPUTypes []runpodGPUType `json:"gpuTypes"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.NewInternalError("runpod", err)
	}

	return result.GPUTypes, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
