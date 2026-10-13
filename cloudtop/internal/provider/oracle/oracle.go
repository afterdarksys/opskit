package oracle

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/errors"
	"github.com/afterdarksys/opskit/cloudtop/internal/metrics"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
	"github.com/afterdarksys/opskit/cloudtop/pkg/ratelimit"
)

func init() {
	provider.Register("oracle", func() provider.Provider {
		return &OracleProvider{}
	})
}

// OCI pricing table (hourly rates in USD for commercial regions)
var ociPricing = map[string]float64{
	"VM.Standard2.1":  0.0550,
	"VM.Standard2.2":  0.1100,
	"VM.Standard2.4":  0.2200,
	"VM.Standard2.8":  0.4400,
	"VM.Standard2.16": 0.8800,
	"VM.Standard2.24": 1.3200,
	"VM.Standard3.Flex": 0.0150,
	"VM.Standard.E2.1":  0.0380,
	"VM.Standard.E2.2":  0.0760,
	"VM.Standard.E2.4":  0.1520,
	"VM.Standard.E2.8":  0.3040,
	"VM.Standard.E3.Flex": 0.0150,
	"VM.Standard.E4.Flex": 0.0120,
	"VM.DenseIO2.8":  0.6800,
	"VM.DenseIO2.16": 1.3600,
	"VM.DenseIO2.24": 2.0400,
	"VM.Optimized3.Flex": 0.0180,
	"VM.GPU2.1": 1.2750,
	"VM.GPU3.1": 3.0600,
	"VM.GPU3.2": 6.1200,
	"VM.GPU3.4": 12.2400,
	"BM.GPU3.8": 24.4800,
	"BM.GPU4.8": 32.7700,
	"BM.GPU.A100-v2.8": 40.9600,
	"BM.Standard2.52": 3.3400,
	"BM.Standard.E3.128": 6.5280,
	"BM.Standard.E4.128": 5.2224,
	"VM.Standard.A1.Flex": 0.0015,
	"VM.Standard.E2.1.Micro": 0.0000,
	"VM.Standard.A1.Flex.1":  0.0000,
}

func getShapePricing(shape string) float64 {
	if price, ok := ociPricing[shape]; ok {
		return price
	}
	if strings.Contains(shape, ".Flex") {
		baseShape := strings.Split(shape, ".")[0] + "." + strings.Split(shape, ".")[1] + ".Flex"
		if basePrice, ok := ociPricing[baseShape]; ok {
			return basePrice
		}
	}
	if strings.Contains(shape, "GPU") {
		return 3.0
	} else if strings.Contains(shape, "BM.") {
		return 3.0
	} else if strings.Contains(shape, "A1") {
		return 0.0015
	}
	return 0.05
}

// OracleProvider implements Provider and GPUProvider interfaces
type OracleProvider struct {
	config        *provider.ProviderConfig
	tenancyID     string
	userID        string
	fingerprint   string
	privateKey    *rsa.PrivateKey
	region        string
	compartmentID string
	client        *http.Client
	limiter       *ratelimit.Limiter
}

func (p *OracleProvider) Name() string {
	return "oracle"
}

func (p *OracleProvider) Initialize(ctx context.Context, config *provider.ProviderConfig) error {
	p.config = config

	// Get key file path from credentials
	keyFile, ok := config.Credentials["key_file"]
	if !ok || keyFile == "" {
		return errors.NewAuthError("oracle", fmt.Errorf("missing key_file"))
	}

	// Expand home directory
	if keyFile[0] == '~' {
		home, _ := os.UserHomeDir()
		keyFile = filepath.Join(home, keyFile[1:])
	}

	// Parse OCI config file
	if err := p.parseOCIConfig(keyFile); err != nil {
		return errors.NewAuthError("oracle", err)
	}

	// Get compartment ID from options or use tenancy
	if compartmentID, ok := config.Options["compartment_id"].(string); ok {
		p.compartmentID = compartmentID
	} else {
		p.compartmentID = p.tenancyID
	}

	// Override region if specified
	if region, ok := config.Options["region"].(string); ok {
		p.region = region
	}

	// Create HTTP client with IPv4-only transport (OCI has IPv6 issues)
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	p.client = &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	// Set up rate limiter
	if config.RateLimit != nil {
		p.limiter = ratelimit.NewLimiter(
			config.RateLimit.RequestsPerSecond,
			config.RateLimit.Burst,
			config.RateLimit.Timeout,
		)
	} else {
		// Default rate limit for OCI
		p.limiter = ratelimit.NewLimiter(10, 20, 30*time.Second)
	}

	return nil
}

func (p *OracleProvider) parseOCIConfig(configPath string) error {
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("failed to open OCI config: %w", err)
	}
	defer file.Close()

	config := make(map[string]string)
	scanner := bufio.NewScanner(file)
	currentProfile := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentProfile = strings.Trim(line, "[]")
			continue
		}

		if currentProfile == "DEFAULT" || currentProfile == "" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				config[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	p.tenancyID = config["tenancy"]
	p.userID = config["user"]
	p.fingerprint = config["fingerprint"]
	p.region = config["region"]

	// Load private key
	keyPath := config["key_file"]
	if keyPath != "" {
		if keyPath[0] == '~' {
			home, _ := os.UserHomeDir()
			keyPath = filepath.Join(home, keyPath[1:])
		}
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("failed to read private key: %w", err)
		}

		block, _ := pem.Decode(keyData)
		if block == nil {
			return fmt.Errorf("failed to parse PEM block")
		}

		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			// Try PKCS1
			key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return fmt.Errorf("failed to parse private key: %w", err)
			}
		}

		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			p.privateKey = rsaKey
		} else {
			return fmt.Errorf("private key is not RSA")
		}
	}

	return nil
}

func (p *OracleProvider) HealthCheck(ctx context.Context) error {
	if err := p.limiter.Wait(ctx); err != nil {
		return errors.NewRateLimitError("oracle", err)
	}

	// Test by listing availability domains
	_, err := p.listAvailabilityDomains(ctx)
	if err != nil {
		return errors.NewAuthError("oracle", err)
	}

	return nil
}

func (p *OracleProvider) ListServices(ctx context.Context) ([]provider.Service, error) {
	return []provider.Service{
		{ID: "compute", Name: "Compute Instances", Type: "compute", Capabilities: []string{"compute", "metrics", "gpu"}},
		{ID: "containers", Name: "Container Engine (OKE)", Type: "containers", Capabilities: []string{"containers", "kubernetes"}},
		{ID: "autonomous_db", Name: "Autonomous Database", Type: "database", Capabilities: []string{"database", "metrics"}},
		{ID: "object_storage", Name: "Object Storage", Type: "storage", Capabilities: []string{"storage", "metrics"}},
		{ID: "functions", Name: "Functions", Type: "serverless", Capabilities: []string{"compute", "metrics"}},
	}, nil
}

func (p *OracleProvider) ListResources(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Resource, error) {
	var resources []provider.Resource

	// List compute instances
	if filter == nil || len(filter.Types) == 0 || contains(filter.Types, "compute") {
		instances, err := p.listInstances(ctx, filter)
		if err == nil {
			for _, inst := range instances {
				resources = append(resources, inst.Resource)
			}
		}
	}

	return resources, nil
}

func (p *OracleProvider) GetMetrics(ctx context.Context, req *provider.MetricsRequest) (*provider.MetricsResponse, error) {
	return &provider.MetricsResponse{
		Provider:  "oracle",
		Metrics:   make(map[string]interface{}),
		Timestamp: time.Now(),
		Cached:    false,
	}, nil
}

func (p *OracleProvider) Close() error {
	return nil
}

// ComputeProvider interface
func (p *OracleProvider) ListInstances(ctx context.Context, filter *provider.InstanceFilter) ([]provider.Instance, error) {
	return p.listInstances(ctx, &filter.ResourceFilter)
}

func (p *OracleProvider) GetInstanceMetrics(ctx context.Context, instanceID string) (*metrics.ComputeMetrics, error) {
	return &metrics.ComputeMetrics{
		ResourceID: instanceID,
		Provider:   "oracle",
		Timestamp:  time.Now(),
	}, nil
}

// GPUProvider interface
func (p *OracleProvider) ListGPUInstances(ctx context.Context, filter *provider.GPUFilter) ([]provider.GPUInstance, error) {
	instances, err := p.listInstances(ctx, &filter.ResourceFilter)
	if err != nil {
		return nil, err
	}

	var gpuInstances []provider.GPUInstance
	for _, inst := range instances {
		if isGPUShape(inst.InstanceType) {
			gpuInfo := parseGPUShape(inst.InstanceType)
			gpuInstance := provider.GPUInstance{
				Instance:    inst,
				GPUType:     gpuInfo.gpuType,
				GPUCount:    gpuInfo.gpuCount,
				GPUMemoryGB: gpuInfo.gpuMemoryGB,
			}
			gpuInstances = append(gpuInstances, gpuInstance)
		}
	}

	return gpuInstances, nil
}

func (p *OracleProvider) GetGPUMetrics(ctx context.Context, instanceID string) (*metrics.GPUMetrics, error) {
	return &metrics.GPUMetrics{
		ResourceID: instanceID,
		Provider:   "oracle",
		Timestamp:  time.Now(),
		GPUs:       []metrics.GPUDeviceMetrics{},
	}, nil
}

func (p *OracleProvider) GetGPUAvailability(ctx context.Context) ([]provider.GPUOffering, error) {
	shapes, err := p.listShapes(ctx)
	if err != nil {
		return nil, err
	}

	var offerings []provider.GPUOffering
	for _, shape := range shapes {
		if isGPUShape(shape.Name) {
			gpuInfo := parseGPUShape(shape.Name)
			offering := provider.GPUOffering{
				Provider:     "oracle",
				GPUType:      gpuInfo.gpuType,
				GPUCount:     gpuInfo.gpuCount,
				GPUMemoryGB:  gpuInfo.gpuMemoryGB,
				CPUCores:     shape.Ocpus,
				MemoryGB:     shape.MemoryGB,
				Available:    true,
				Region:       p.region,
				InstanceType: shape.Name,
			}
			offerings = append(offerings, offering)
		}
	}

	return offerings, nil
}

// OCI API types
type ociInstance struct {
	ID             string    `json:"id"`
	DisplayName    string    `json:"displayName"`
	Shape          string    `json:"shape"`
	LifecycleState string    `json:"lifecycleState"`
	Region         string    `json:"region"`
	TimeCreated    time.Time `json:"timeCreated"`
}

type ociShape struct {
	Name     string  `json:"shape"`
	Ocpus    int     `json:"ocpus"`
	MemoryGB float64 `json:"memoryInGBs"`
}

type ociAvailabilityDomain struct {
	Name string `json:"name"`
}

// Private methods
func (p *OracleProvider) getBaseURL(service string) string {
	return fmt.Sprintf("https://%s.%s.oci.oraclecloud.com", service, p.region)
}

func (p *OracleProvider) doRequest(ctx context.Context, method, requestURL string) ([]byte, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, errors.NewRateLimitError("oracle", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, errors.NewInternalError("oracle", err)
	}

	// Sign the request with OCI authentication
	if err := p.signRequest(req); err != nil {
		return nil, errors.NewAuthError("oracle", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errors.NewNetworkError("oracle", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewNetworkError("oracle", err)
	}

	if resp.StatusCode >= 400 {
		return nil, errors.NewNetworkError("oracle", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body)))
	}

	return body, nil
}

// signRequest signs an HTTP request for OCI authentication
func (p *OracleProvider) signRequest(req *http.Request) error {
	// Required headers for GET requests
	requiredHeaders := []string{"date", "(request-target)", "host"}

	// Set date header
	dateStr := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("Date", dateStr)
	req.Header.Set("Content-Type", "application/json")

	// Parse URL for host
	parsedURL, err := url.Parse(req.URL.String())
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}
	req.Header.Set("Host", parsedURL.Host)

	// Build the signing string
	var signingParts []string
	for _, header := range requiredHeaders {
		var value string
		switch header {
		case "(request-target)":
			path := parsedURL.Path
			if parsedURL.RawQuery != "" {
				path += "?" + parsedURL.RawQuery
			}
			value = fmt.Sprintf("%s: %s %s", header, strings.ToLower(req.Method), path)
		case "host":
			value = fmt.Sprintf("%s: %s", header, parsedURL.Host)
		case "date":
			value = fmt.Sprintf("%s: %s", header, dateStr)
		default:
			value = fmt.Sprintf("%s: %s", header, req.Header.Get(strings.Title(header)))
		}
		signingParts = append(signingParts, value)
	}

	signingString := strings.Join(signingParts, "\n")

	// Hash and sign
	hashed := sha256.Sum256([]byte(signingString))
	signature, err := rsa.SignPKCS1v15(rand.Reader, p.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	// Build authorization header
	keyID := fmt.Sprintf("%s/%s/%s", p.tenancyID, p.userID, p.fingerprint)
	authHeader := fmt.Sprintf(
		`Signature version="1",keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		keyID,
		strings.Join(requiredHeaders, " "),
		base64.StdEncoding.EncodeToString(signature),
	)

	req.Header.Set("Authorization", authHeader)
	return nil
}

func (p *OracleProvider) listAvailabilityDomains(ctx context.Context) ([]ociAvailabilityDomain, error) {
	url := fmt.Sprintf("%s/20160918/availabilityDomains?compartmentId=%s",
		p.getBaseURL("identity"), p.compartmentID)

	body, err := p.doRequest(ctx, "GET", url)
	if err != nil {
		return nil, err
	}

	var domains []ociAvailabilityDomain
	if err := json.Unmarshal(body, &domains); err != nil {
		return nil, errors.NewInternalError("oracle", err)
	}

	return domains, nil
}

func (p *OracleProvider) listInstances(ctx context.Context, filter *provider.ResourceFilter) ([]provider.Instance, error) {
	url := fmt.Sprintf("%s/20160918/instances?compartmentId=%s",
		p.getBaseURL("iaas"), p.compartmentID)

	body, err := p.doRequest(ctx, "GET", url)
	if err != nil {
		// Return empty for demo if API fails
		return []provider.Instance{}, nil
	}

	var ociInstances []ociInstance
	if err := json.Unmarshal(body, &ociInstances); err != nil {
		return nil, errors.NewInternalError("oracle", err)
	}

	var instances []provider.Instance
	for _, inst := range ociInstances {
		instance := provider.Instance{
			Resource: provider.Resource{
				ID:        inst.ID,
				Name:      inst.DisplayName,
				Type:      "compute",
				Provider:  "oracle",
				Region:    inst.Region,
				Status:    strings.ToLower(inst.LifecycleState),
				CreatedAt: inst.TimeCreated,
				HourlyRate: getShapePricing(inst.Shape),
			},
			InstanceType: inst.Shape,
			State:        strings.ToLower(inst.LifecycleState),
		}

		if filter != nil && len(filter.Status) > 0 && !contains(filter.Status, instance.Status) {
			continue
		}

		instances = append(instances, instance)
	}

	return instances, nil
}

func (p *OracleProvider) listShapes(ctx context.Context) ([]ociShape, error) {
	url := fmt.Sprintf("%s/20160918/shapes?compartmentId=%s",
		p.getBaseURL("iaas"), p.compartmentID)

	body, err := p.doRequest(ctx, "GET", url)
	if err != nil {
		return nil, err
	}

	var shapes []ociShape
	if err := json.Unmarshal(body, &shapes); err != nil {
		return nil, errors.NewInternalError("oracle", err)
	}

	return shapes, nil
}

type gpuShapeInfo struct {
	gpuType     string
	gpuCount    int
	gpuMemoryGB float64
}

func isGPUShape(shape string) bool {
	return strings.Contains(strings.ToUpper(shape), "GPU")
}

func parseGPUShape(shape string) gpuShapeInfo {
	info := gpuShapeInfo{
		gpuType:     "Unknown",
		gpuCount:    1,
		gpuMemoryGB: 16,
	}

	upper := strings.ToUpper(shape)
	if strings.Contains(upper, "GPU3") {
		info.gpuType = "NVIDIA V100"
		info.gpuMemoryGB = 16
	} else if strings.Contains(upper, "GPU4") || strings.Contains(upper, "A100") {
		info.gpuType = "NVIDIA A100"
		info.gpuMemoryGB = 80
	} else if strings.Contains(upper, "A10") {
		info.gpuType = "NVIDIA A10"
		info.gpuMemoryGB = 24
	}

	// Parse count from shape name
	for _, char := range shape[len(shape)-1:] {
		if char >= '1' && char <= '9' {
			info.gpuCount = int(char - '0')
			break
		}
	}

	return info
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
