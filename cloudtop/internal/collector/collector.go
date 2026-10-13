package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/output"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
)

// Collector orchestrates data collection from multiple providers
type Collector struct {
	providers map[string]provider.Provider
	cache     Cache
}

// CollectRequest specifies what to collect
type CollectRequest struct {
	Providers   []string
	Services    []string
	MetricTypes []string
	Filters     *provider.ResourceFilter
	Timeout     time.Duration
}

// NewCollector creates a new collector instance
func NewCollector(providers map[string]provider.Provider, cache Cache) *Collector {
	return &Collector{
		providers: providers,
		cache:     cache,
	}
}

// Collect gathers data from all specified providers concurrently
func (c *Collector) Collect(ctx context.Context, req *CollectRequest) (*output.CollectResult, error) {
	start := time.Now()

	// Set default timeout if not specified
	if req.Timeout == 0 {
		req.Timeout = 60 * time.Second
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	// Determine which providers to query
	providersToQuery := c.getProvidersToQuery(req.Providers)

	// Collect from providers concurrently
	results := make(map[string]*output.ProviderResult)
	errors := make(map[string]error)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, providerName := range providersToQuery {
		wg.Add(1)

		go func(name string) {
			defer wg.Done()

			result, err := c.collectFromProvider(ctx, name, req)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errors[name] = err
			} else {
				results[name] = result
			}
		}(providerName)
	}

	wg.Wait()

	return &output.CollectResult{
		Results:   results,
		Errors:    errors,
		Timestamp: time.Now(),
		Duration:  time.Since(start),
	}, nil
}

// CollectGPU collects GPU instances from all GPU providers
func (c *Collector) CollectGPU(ctx context.Context, filter *provider.GPUFilter) ([]provider.GPUInstance, map[string]error) {
	var allInstances []provider.GPUInstance
	errors := make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, p := range c.providers {
		gpuProvider, ok := p.(provider.GPUProvider)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(name string, gp provider.GPUProvider) {
			defer wg.Done()

			instances, err := gp.ListGPUInstances(ctx, filter)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errors[name] = err
			} else {
				allInstances = append(allInstances, instances...)
			}
		}(name, gpuProvider)
	}

	wg.Wait()
	return allInstances, errors
}

// CollectGPUAvailability collects GPU offerings from all GPU providers
func (c *Collector) CollectGPUAvailability(ctx context.Context) ([]provider.GPUOffering, map[string]error) {
	var allOfferings []provider.GPUOffering
	errors := make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, p := range c.providers {
		gpuProvider, ok := p.(provider.GPUProvider)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(name string, gp provider.GPUProvider) {
			defer wg.Done()

			offerings, err := gp.GetGPUAvailability(ctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errors[name] = err
			} else {
				allOfferings = append(allOfferings, offerings...)
			}
		}(name, gpuProvider)
	}

	wg.Wait()
	return allOfferings, errors
}

// collectFromProvider collects data from a single provider
func (c *Collector) collectFromProvider(ctx context.Context, providerName string, req *CollectRequest) (*output.ProviderResult, error) {
	start := time.Now()

	// Check cache first
	cacheKey := c.buildCacheKey(providerName, req)
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			result := cached.(*output.ProviderResult)
			result.Cached = true
			return result, nil
		}
	}

	// Get provider
	p, ok := c.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", providerName)
	}

	// Check provider health
	if err := p.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	// List resources
	resources, err := p.ListResources(ctx, req.Filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list resources: %w", err)
	}

	// Collect metrics for resources
	metricsData := make(map[string]interface{})

	if len(req.MetricTypes) > 0 && len(resources) > 0 {
		resourceIDs := make([]string, len(resources))
		for i, r := range resources {
			resourceIDs[i] = r.ID
		}

		metricsReq := &provider.MetricsRequest{
			ResourceIDs: resourceIDs,
			MetricNames: req.MetricTypes,
			StartTime:   time.Now().Add(-5 * time.Minute),
			EndTime:     time.Now(),
			Granularity: 1 * time.Minute,
		}

		metricsResp, err := p.GetMetrics(ctx, metricsReq)
		if err == nil {
			metricsData = metricsResp.Metrics
		}
	}

	result := &output.ProviderResult{
		Provider:  providerName,
		Resources: resources,
		Metrics:   metricsData,
		Cached:    false,
		Duration:  time.Since(start),
	}

	// Cache the result
	if c.cache != nil {
		c.cache.Set(cacheKey, result)
	}

	return result, nil
}

// getProvidersToQuery determines which providers to query
func (c *Collector) getProvidersToQuery(requested []string) []string {
	if len(requested) == 0 {
		// Return all providers
		providers := make([]string, 0, len(c.providers))
		for name := range c.providers {
			providers = append(providers, name)
		}
		return providers
	}
	return requested
}

// buildCacheKey creates a cache key from request parameters
func (c *Collector) buildCacheKey(provider string, req *CollectRequest) string {
	return fmt.Sprintf("%s:%v:%v", provider, req.Services, req.MetricTypes)
}

// GetProvider returns a specific provider by name
func (c *Collector) GetProvider(name string) (provider.Provider, bool) {
	p, ok := c.providers[name]
	return p, ok
}

// GetProviders returns all providers
func (c *Collector) GetProviders() map[string]provider.Provider {
	return c.providers
}
