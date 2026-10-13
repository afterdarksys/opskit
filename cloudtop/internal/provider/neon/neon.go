package neon

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
	provider.Register("neon", func() provider.Provider {
		return &NeonProvider{}
	})
}

// NeonProvider implements the Provider and DatabaseProvider interfaces
type NeonProvider struct {
	config   *provider.ProviderConfig
	apiKey   string
	client   *http.Client
	limiter  *ratelimit.Limiter
}

const baseURL = "https://console.neon.tech/api/v2"

func (p *NeonProvider) Name() string {
	return "neon"
}

func (p *NeonProvider) Initialize(ctx context.Context, config *provider.ProviderConfig) error {
	p.config = config

	// Get API key from credentials
	apiKey, ok := config.Credentials["api_token"]
	if !ok || apiKey == "" {
		return errors.NewAuthError("neon", fmt.Errorf("missing api_token (NEON_API_KEY)"))
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
		// Default rate limit for Neon API
		p.limiter = ratelimit.NewLimiter(10, 20, 30*time.Second)
	}

	return nil
}

func (p *NeonProvider) HealthCheck(ctx context.Context) error {
	if err := p.limiter.Wait(ctx); err != nil {
		return errors.NewRateLimitError("neon", err)
	}

	// Verify by listing projects
	_, err := p.listProjects(ctx)
	return err
}

func (p *NeonProvider) ListServices(ctx context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "projects", Name: "Neon Projects", Type: "database", Capabilities: []string{"database", "metrics"}},
		{ID: "branches", Name: "Database Branches", Type: "database", Capabilities: []string{"database", "branching"}},
		{ID: "endpoints", Name: "Compute Endpoints", Type: "compute", Capabilities: []string{"compute", "metrics"}},
	}, nil
}

func (p *NeonProvider) ListResources(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Resource, error) {
	var resources []provider.Resource

	// List projects
	projects, err := p.listProjects(ctx)
	if err != nil {
		return nil, err
	}

	for _, proj := range projects {
		resources = append(resources, provider.Resource{
			ID:        proj.ID,
			Name:      proj.Name,
			Type:      "project",
			Provider:  "neon",
			Region:    proj.RegionID,
			Status:    "active",
			CreatedAt: proj.CreatedAt,
			UpdatedAt: proj.UpdatedAt,
		})
	}

	// List endpoints for each project
	for _, proj := range projects {
		endpoints, err := p.listEndpoints(ctx, proj.ID)
		if err != nil {
			continue
		}
		for _, ep := range endpoints {
			status := "active"
			if ep.Disabled {
				status = "disabled"
			}
			resources = append(resources, provider.Resource{
				ID:        ep.ID,
				Name:      ep.ID,
				Type:      "endpoint",
				Provider:  "neon",
				Region:    ep.RegionID,
				Status:    status,
				CreatedAt: ep.CreatedAt,
				UpdatedAt: ep.UpdatedAt,
			})
		}
	}

	return resources, nil
}

func (p *NeonProvider) GetMetrics(ctx context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	metricsData := make(map[string]interface{})

	for _, resourceID := range req.ResourceIDs {
		// Get project consumption data
		consumption, err := p.getProjectConsumption(ctx, resourceID)
		if err == nil {
			metricsData[resourceID] = consumption
		}
	}

	return &provider.MetricsResponse{
		Provider:  "neon",
		Metrics:   metricsData,
		Timestamp: time.Now(),
		Cached:    false,
	}, nil
}

func (p *NeonProvider) Close() error {
	return nil
}

// DatabaseProvider interface
func (p *NeonProvider) ListDatabases(ctx context.Context) ([]provider.Database, error) {
	var databases []provider.Database

	projects, err := p.listProjects(ctx)
	if err != nil {
		return nil, err
	}

	for _, proj := range projects {
		branches, err := p.listBranches(ctx, proj.ID)
		if err != nil {
			continue
		}

		for _, branch := range branches {
			databases = append(databases, provider.Database{
				Resource: provider.Resource{
					ID:        branch.ID,
					Name:      branch.Name,
					Type:      "database",
					Provider:  "neon",
					Region:    proj.RegionID,
					Status:    "active",
					CreatedAt: branch.CreatedAt,
					UpdatedAt: branch.UpdatedAt,
				},
				Engine:   "postgres",
				Version:  proj.PgVersion,
				Endpoint: proj.ID + "." + proj.RegionID + ".neon.tech",
			})
		}
	}

	return databases, nil
}

func (p *NeonProvider) GetDatabaseMetrics(ctx context.Context, dbID string) (*metrics.DatabaseMetrics, error) {
	return &metrics.DatabaseMetrics{
		ResourceID: dbID,
		Provider:   "neon",
		Timestamp:  time.Now(),
	}, nil
}

// API types
type neonProject struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	RegionID        string    `json:"region_id"`
	PgVersion       string    `json:"pg_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type neonBranch struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type neonEndpoint struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	BranchID  string    `json:"branch_id"`
	RegionID  string    `json:"region_id"`
	Host      string    `json:"host"`
	Type      string    `json:"type"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type neonConsumption struct {
	ActiveTimeSeconds int64   `json:"active_time_seconds"`
	ComputeTimeSeconds int64  `json:"compute_time_seconds"`
	DataStorageBytesHour int64 `json:"data_storage_bytes_hour"`
	WrittenDataBytes  int64   `json:"written_data_bytes"`
}

// API response wrapper
type neonResponse struct {
	Projects  []neonProject  `json:"projects,omitempty"`
	Branches  []neonBranch   `json:"branches,omitempty"`
	Endpoints []neonEndpoint `json:"endpoints,omitempty"`
}

func (p *NeonProvider) doRequest(ctx context.Context, method, path string) ([]byte, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, errors.NewRateLimitError("neon", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
	if err != nil {
		return nil, errors.NewInternalError("neon", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errors.NewNetworkError("neon", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewNetworkError("neon", err)
	}

	if resp.StatusCode >= 400 {
		return nil, errors.NewNetworkError("neon", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body)))
	}

	return body, nil
}

func (p *NeonProvider) listProjects(ctx context.Context) ([]neonProject, error) {
	body, err := p.doRequest(ctx, "GET", "/projects")
	if err != nil {
		return nil, err
	}

	var resp neonResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.NewInternalError("neon", err)
	}

	return resp.Projects, nil
}

func (p *NeonProvider) listBranches(ctx context.Context, projectID string) ([]neonBranch, error) {
	body, err := p.doRequest(ctx, "GET", "/projects/"+projectID+"/branches")
	if err != nil {
		return nil, err
	}

	var resp neonResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.NewInternalError("neon", err)
	}

	return resp.Branches, nil
}

func (p *NeonProvider) listEndpoints(ctx context.Context, projectID string) ([]neonEndpoint, error) {
	body, err := p.doRequest(ctx, "GET", "/projects/"+projectID+"/endpoints")
	if err != nil {
		return nil, err
	}

	var resp neonResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.NewInternalError("neon", err)
	}

	return resp.Endpoints, nil
}

func (p *NeonProvider) getProjectConsumption(ctx context.Context, projectID string) (*neonConsumption, error) {
	body, err := p.doRequest(ctx, "GET", "/projects/"+projectID+"/consumption")
	if err != nil {
		return nil, err
	}

	var consumption neonConsumption
	if err := json.Unmarshal(body, &consumption); err != nil {
		return nil, errors.NewInternalError("neon", err)
	}

	return &consumption, nil
}
