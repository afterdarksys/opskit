package cloudflare

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
	provider.Register("cloudflare", func() provider.Provider {
		return &CloudflareProvider{}
	})
}

// CloudflareProvider implements the Provider interface for Cloudflare
type CloudflareProvider struct {
	config    *provider.ProviderConfig
	apiToken  string
	accountID string
	client    *http.Client
	limiter   *ratelimit.Limiter
}

const (
	baseURL = "https://api.cloudflare.com/client/v4"
)

func (p *CloudflareProvider) Name() string {
	return "cloudflare"
}

func (p *CloudflareProvider) Initialize(ctx context.Context, config *provider.ProviderConfig) error {
	p.config = config

	// Get API token from credentials
	apiToken, ok := config.Credentials["api_token"]
	if !ok || apiToken == "" {
		return errors.NewAuthError("cloudflare", fmt.Errorf("missing api_token"))
	}
	p.apiToken = apiToken

	// Get account ID from options
	if accountID, ok := config.Options["account_id"].(string); ok {
		p.accountID = accountID
	}

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
		// Default rate limit for Cloudflare API (1200 req/5min = 4/sec)
		p.limiter = ratelimit.NewLimiter(4, 10, 30*time.Second)
	}

	return nil
}

func (p *CloudflareProvider) HealthCheck(ctx context.Context) error {
	if err := p.limiter.Wait(ctx); err != nil {
		return errors.NewRateLimitError("cloudflare", err)
	}

	// Verify token by calling /user/tokens/verify
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/user/tokens/verify", nil)
	if err != nil {
		return errors.NewInternalError("cloudflare", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return errors.NewNetworkError("cloudflare", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.NewAuthError("cloudflare", fmt.Errorf("token verification failed: %s", string(body)))
	}

	return nil
}

func (p *CloudflareProvider) ListServices(ctx context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "workers", Name: "Cloudflare Workers", Type: "serverless", Capabilities: []string{"compute", "metrics"}},
		{ID: "r2", Name: "R2 Storage", Type: "storage", Capabilities: []string{"storage", "metrics"}},
		{ID: "d1", Name: "D1 Database", Type: "database", Capabilities: []string{"database", "metrics"}},
		{ID: "kv", Name: "Workers KV", Type: "storage", Capabilities: []string{"storage"}},
		{ID: "ai", Name: "Cloudflare AI", Type: "ai", Capabilities: []string{"ai", "inference"}},
		{ID: "pages", Name: "Cloudflare Pages", Type: "hosting", Capabilities: []string{"hosting", "deployments"}},
	}, nil
}

func (p *CloudflareProvider) ListResources(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Resource, error) {
	var resources []provider.Resource

	// List Workers scripts
	if filter == nil || len(filter.Types) == 0 || contains(filter.Types, "workers") {
		workers, err := p.listWorkers(ctx)
		if err == nil {
			resources = append(resources, workers...)
		}
	}

	// List R2 buckets
	if filter == nil || len(filter.Types) == 0 || contains(filter.Types, "r2") {
		buckets, err := p.listR2Buckets(ctx)
		if err == nil {
			resources = append(resources, buckets...)
		}
	}

	// List D1 databases
	if filter == nil || len(filter.Types) == 0 || contains(filter.Types, "d1") {
		dbs, err := p.listD1Databases(ctx)
		if err == nil {
			resources = append(resources, dbs...)
		}
	}

	return resources, nil
}

func (p *CloudflareProvider) GetMetrics(ctx context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, errors.NewRateLimitError("cloudflare", err)
	}

	metricsData := make(map[string]interface{})

	// For Workers, get analytics
	for _, resourceID := range req.ResourceIDs {
		analytics, err := p.getWorkerAnalytics(ctx, resourceID)
		if err == nil {
			metricsData[resourceID] = analytics
		}
	}

	return &provider.MetricsResponse{
		Provider:  "cloudflare",
		Metrics:   metricsData,
		Timestamp: time.Now(),
		Cached:    false,
	}, nil
}

func (p *CloudflareProvider) Close() error {
	return nil
}

// API response types
type cfResponse struct {
	Success  bool            `json:"success"`
	Errors   []cfError       `json:"errors"`
	Messages []string        `json:"messages"`
	Result   json.RawMessage `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfWorker struct {
	ID         string    `json:"id"`
	CreatedOn  time.Time `json:"created_on"`
	ModifiedOn time.Time `json:"modified_on"`
	Etag       string    `json:"etag"`
}

type cfR2Bucket struct {
	Name         string    `json:"name"`
	CreationDate time.Time `json:"creation_date"`
}

type cfD1Database struct {
	UUID      string    `json:"uuid"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Private helper methods
func (p *CloudflareProvider) doRequest(ctx context.Context, method, path string) (*cfResponse, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, errors.NewRateLimitError("cloudflare", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
	if err != nil {
		return nil, errors.NewInternalError("cloudflare", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errors.NewNetworkError("cloudflare", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewNetworkError("cloudflare", err)
	}

	var cfResp cfResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return nil, errors.NewInternalError("cloudflare", fmt.Errorf("failed to parse response: %w", err))
	}

	if !cfResp.Success && len(cfResp.Errors) > 0 {
		return nil, errors.NewNetworkError("cloudflare", fmt.Errorf("API error: %s", cfResp.Errors[0].Message))
	}

	return &cfResp, nil
}

func (p *CloudflareProvider) listWorkers(ctx context.Context) ([]provider.Resource, error) {
	if p.accountID == "" {
		return nil, errors.NewValidationError("cloudflare", "account_id required for Workers")
	}

	cfResp, err := p.doRequest(ctx, "GET", "/accounts/"+p.accountID+"/workers/scripts")
	if err != nil {
		return nil, err
	}

	var workers []cfWorker
	if err := json.Unmarshal(cfResp.Result, &workers); err != nil {
		return nil, errors.NewInternalError("cloudflare", err)
	}

	resources := make([]provider.Resource, 0, len(workers))
	for _, w := range workers {
		resources = append(resources, provider.Resource{
			ID:        w.ID,
			Name:      w.ID,
			Type:      "workers",
			Provider:  "cloudflare",
			Region:    "global",
			Status:    "active",
			CreatedAt: w.CreatedOn,
			UpdatedAt: w.ModifiedOn,
		})
	}

	return resources, nil
}

func (p *CloudflareProvider) listR2Buckets(ctx context.Context) ([]provider.Resource, error) {
	if p.accountID == "" {
		return nil, errors.NewValidationError("cloudflare", "account_id required for R2")
	}

	cfResp, err := p.doRequest(ctx, "GET", "/accounts/"+p.accountID+"/r2/buckets")
	if err != nil {
		return nil, err
	}

	var buckets []cfR2Bucket
	if err := json.Unmarshal(cfResp.Result, &buckets); err != nil {
		return nil, errors.NewInternalError("cloudflare", err)
	}

	resources := make([]provider.Resource, 0, len(buckets))
	for _, b := range buckets {
		resources = append(resources, provider.Resource{
			ID:        b.Name,
			Name:      b.Name,
			Type:      "r2",
			Provider:  "cloudflare",
			Region:    "global",
			Status:    "active",
			CreatedAt: b.CreationDate,
		})
	}

	return resources, nil
}

func (p *CloudflareProvider) listD1Databases(ctx context.Context) ([]provider.Resource, error) {
	if p.accountID == "" {
		return nil, errors.NewValidationError("cloudflare", "account_id required for D1")
	}

	cfResp, err := p.doRequest(ctx, "GET", "/accounts/"+p.accountID+"/d1/database")
	if err != nil {
		return nil, err
	}

	var dbs []cfD1Database
	if err := json.Unmarshal(cfResp.Result, &dbs); err != nil {
		return nil, errors.NewInternalError("cloudflare", err)
	}

	resources := make([]provider.Resource, 0, len(dbs))
	for _, db := range dbs {
		resources = append(resources, provider.Resource{
			ID:        db.UUID,
			Name:      db.Name,
			Type:      "d1",
			Provider:  "cloudflare",
			Region:    "global",
			Status:    "active",
			CreatedAt: db.CreatedAt,
		})
	}

	return resources, nil
}

func (p *CloudflareProvider) getWorkerAnalytics(ctx context.Context, scriptName string) (*metrics.FunctionMetrics, error) {
	// Cloudflare Workers analytics via GraphQL API
	// This is a simplified implementation
	return &metrics.FunctionMetrics{
		ResourceID: scriptName,
		Provider:   "cloudflare",
		Timestamp:  time.Now(),
	}, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
