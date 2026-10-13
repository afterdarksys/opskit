package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/afterdarksys/opskit/cloudtop/internal/collector"
	"github.com/afterdarksys/opskit/cloudtop/internal/config"
	"github.com/afterdarksys/opskit/cloudtop/internal/output"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
	"github.com/afterdarksys/opskit/cloudtop/internal/server"

	// Import all providers to register them
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/azure"
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/baremetal"
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/cloudflare"
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/gcp"
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/neon"
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/oracle"
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/runpod"
	_ "github.com/afterdarksys/opskit/cloudtop/internal/provider/vastai"
)

var (
	cfgFile string
	cfg     *config.Config

	// Provider flags
	flagCloudflare bool
	flagOracle     bool
	flagAzure      bool
	flagGCP        bool
	flagNeon       bool
	flagAll        bool

	// Service flags
	flagService string

	// AI/GPU flags
	flagAI  string
	flagGPU bool

	// List flags
	flagList     bool
	flagProvider string
	flagRunning  bool
	flagAllRes   bool

	// Output flags
	flagTable bool
	flagWide  bool
	flagJSON  bool

	// Other flags
	flagRefresh time.Duration

	// Serve flags
	flagServePort    int
	flagServeHost    string
	flagServeToken   string
	flagServeFormat  string
	flagServeRefresh time.Duration
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "cloudtop",
	Short: "Multi-cloud monitoring CLI",
	Long: `cloudtop is a comprehensive CLI tool for monitoring resources across
multiple cloud providers including Cloudflare, Oracle Cloud, Azure, GCP,
Neon, and AI/GPU providers like Vast.ai and RunPod.

Examples:
  # Show all resources from all configured providers
  cloudtop --all

  # Show only Cloudflare resources
  cloudtop --cloudflare

  # Show Oracle Cloud compute instances
  cloudtop --oracle --service compute

  # Show GPU instances from AI providers
  cloudtop --ai vast --gpu

  # List available GPU compute
  cloudtop --gpu --list

  # Output in JSON format
  cloudtop --all --json

  # Auto-refresh every 30 seconds
  cloudtop --all --refresh 30s`,
	RunE: runMonitor,
}

var initConfigCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a sample configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		sampleCfg := config.GenerateSampleConfig()

		path := "cloudtop.json"
		if cfgFile != "" {
			path = cfgFile
		}

		if err := sampleCfg.Save(path); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("Sample configuration written to: %s\n", path)
		fmt.Println("\nEdit this file to add your API keys and configure providers.")
		fmt.Println("You can also set credentials via environment variables:")
		fmt.Println("  CLOUDFLARE_API_TOKEN, NEON_API_KEY, VASTAI_API_KEY, RUNPOD_API_KEY")
		return nil
	},
}

var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "List registered providers",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Registered providers:")
		for _, name := range provider.ListRegistered() {
			status := "available"
			if cfg != nil {
				if p, ok := cfg.Providers[name]; ok && p.Enabled {
					status = "enabled"
				}
			}
			fmt.Printf("  - %s (%s)\n", name, status)
		}
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run cloudtop as a metrics server",
	Long: `Start the cloudtop metrics daemon.

The server collects metrics from all configured providers on a regular interval
and exposes them over HTTP in two formats:

  Prometheus text format  (default: /metrics)
  JSON snapshot           (default: /api/v1/snapshot)

Examples:
  # Start server with defaults from cloudtop.json
  cloudtop serve

  # Listen on a specific port and refresh every minute
  cloudtop serve --port 9091 --refresh 1m

  # Prometheus-only output with bearer token auth
  cloudtop serve --format prometheus --token my-secret

  # JSON-only, bind to localhost only
  cloudtop serve --host 127.0.0.1 --format json`,
	RunE: runServe,
}

func runServe(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Apply CLI flag overrides to the server config
	srvCfg := &cfg.Server
	if flagServePort > 0 {
		srvCfg.Port = flagServePort
	}
	if flagServeHost != "" {
		srvCfg.Host = flagServeHost
	}
	if flagServeToken != "" {
		srvCfg.Auth.Enabled = true
		srvCfg.Auth.Token = flagServeToken
	}
	if flagServeFormat != "" {
		srvCfg.Export.Format = flagServeFormat
	}
	if flagServeRefresh > 0 {
		srvCfg.RefreshInterval = config.Duration(flagServeRefresh)
	}

	// Apply port/refresh defaults if still zero after config load
	if srvCfg.Port == 0 {
		srvCfg.Port = 9090
	}
	if srvCfg.RefreshInterval == 0 {
		srvCfg.RefreshInterval = config.Duration(30 * time.Second)
	}
	if srvCfg.Export.Format == "" {
		srvCfg.Export.Format = "both"
	}

	// Determine which providers to initialise — use all enabled ones plus baremetal
	providerNames := cfg.GetEnabledProviders()
	// Inject baremetal hosts from config as a provider option
	if len(cfg.BaremetalHosts) > 0 {
		providerNames = appendIfMissing(providerNames, "baremetal")
		injectBaremetalHosts(cfg)
	}

	providers, err := initializeProviders(ctx, providerNames)
	if err != nil {
		return fmt.Errorf("failed to initialize providers: %w", err)
	}
	defer closeProviders(providers)

	if len(providers) == 0 {
		fmt.Println("Warning: no providers initialised. Serving empty metrics.")
	}

	srv := server.New(cfg, providers)
	return srv.Start(ctx)
}

// appendIfMissing adds s to slice if not already present.
func appendIfMissing(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// injectBaremetalHosts wires the BaremetalHosts config into the baremetal provider options.
func injectBaremetalHosts(c *config.Config) {
	if _, exists := c.Providers["baremetal"]; !exists {
		c.Providers["baremetal"] = config.Provider{Enabled: true}
	}
	p := c.Providers["baremetal"]
	if p.Options == nil {
		p.Options = make(map[string]interface{})
	}
	hosts := make([]interface{}, 0, len(c.BaremetalHosts))
	for _, h := range c.BaremetalHosts {
		if !h.Enabled {
			continue
		}
		entry := map[string]interface{}{
			"name":   h.Name,
			"labels": h.Labels,
		}
		if h.Host != "" {
			entry["host"] = h.Host
		}
		hosts = append(hosts, entry)
	}
	p.Options["hosts"] = hosts
	c.Providers["baremetal"] = p
}

func init() {
	cobra.OnInitialize(initConfig)

	// Config file
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./cloudtop.json or ~/.cloudtop.json)")

	// Provider flags
	rootCmd.Flags().BoolVarP(&flagCloudflare, "cloudflare", "c", false, "Show Cloudflare resources")
	rootCmd.Flags().BoolVarP(&flagOracle, "oracle", "o", false, "Show Oracle Cloud resources")
	rootCmd.Flags().BoolVar(&flagAzure, "azure", false, "Show Azure resources")
	rootCmd.Flags().BoolVarP(&flagGCP, "gcp", "g", false, "Show GCP resources")
	rootCmd.Flags().BoolVarP(&flagNeon, "neon", "n", false, "Show Neon databases")
	rootCmd.Flags().BoolVarP(&flagAll, "all", "a", false, "Show all providers")

	// Service flags
	rootCmd.Flags().StringVarP(&flagService, "service", "s", "", "Filter by specific service (e.g., compute, storage, workers)")

	// AI/GPU flags
	rootCmd.Flags().StringVar(&flagAI, "ai", "", "Show AI workloads (vast|io|cf|oracle)")
	rootCmd.Flags().BoolVar(&flagGPU, "gpu", false, "Show GPU information")

	// List flags
	rootCmd.Flags().BoolVar(&flagList, "list", false, "List available compute resources")
	rootCmd.Flags().StringVar(&flagProvider, "provider", "", "Filter by provider when using --running or --all")
	rootCmd.Flags().BoolVar(&flagRunning, "running", false, "Show only running resources")
	rootCmd.Flags().BoolVar(&flagAllRes, "all-resources", false, "Show all resources (running and stopped)")

	// Output flags
	rootCmd.Flags().BoolVar(&flagTable, "table", false, "Output in table format (default)")
	rootCmd.Flags().BoolVar(&flagWide, "wide", false, "Output in wide table format")
	rootCmd.Flags().BoolVar(&flagJSON, "json", false, "Output in JSON format")

	// Other flags
	rootCmd.Flags().DurationVar(&flagRefresh, "refresh", 0, "Auto-refresh interval (e.g., 30s, 1m)")

	// Serve subcommand flags
	serveCmd.Flags().IntVar(&flagServePort, "port", 0, "Port to listen on (overrides config, default 9090)")
	serveCmd.Flags().StringVar(&flagServeHost, "host", "", "Bind address (overrides config, default 0.0.0.0)")
	serveCmd.Flags().StringVar(&flagServeToken, "token", "", "Bearer token for auth (empty = no auth)")
	serveCmd.Flags().StringVar(&flagServeFormat, "format", "", "Export format: prometheus, json, or both (overrides config)")
	serveCmd.Flags().DurationVar(&flagServeRefresh, "refresh", 0, "Collection refresh interval (overrides config, e.g. 30s)")

	// Add subcommands
	rootCmd.AddCommand(initConfigCmd)
	rootCmd.AddCommand(providersCmd)
	rootCmd.AddCommand(serveCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Search in current directory and home
		viper.AddConfigPath(".")
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(home)
		}
		viper.SetConfigName("cloudtop")
		viper.SetConfigType("json")
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix("CLOUDTOP")

	if err := viper.ReadInConfig(); err == nil {
		// Config file found
	}

	// Load config
	var err error
	cfg, err = config.Load(viper.ConfigFileUsed())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
		cfg = config.DefaultConfig()
	}
}

func runMonitor(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Determine which providers to query
	providersToQuery := getProvidersFromFlags()
	if len(providersToQuery) == 0 {
		providersToQuery = cfg.GetEnabledProviders()
	}

	if len(providersToQuery) == 0 {
		fmt.Println("No providers configured. Run 'cloudtop init' to generate a config file.")
		fmt.Println("Or specify providers with --cloudflare, --oracle, --neon, etc.")
		return nil
	}

	// Initialize providers
	providers, err := initializeProviders(ctx, providersToQuery)
	if err != nil {
		return fmt.Errorf("failed to initialize providers: %w", err)
	}
	defer closeProviders(providers)

	// Create collector
	var cache collector.Cache
	if cfg.Cache.Enabled {
		cache = collector.NewMemoryCache(cfg.Cache.TTL.Duration(), cfg.Cache.MaxSize)
	} else {
		cache = collector.NewNoopCache()
	}
	col := collector.NewCollector(providers, cache)

	// Handle GPU-specific commands
	if flagGPU && flagList {
		return runGPUList(ctx, col)
	}
	if flagGPU {
		return runGPUInstances(ctx, col)
	}

	// Run collection loop
	if flagRefresh > 0 {
		return runContinuous(ctx, col)
	}
	return runOnce(ctx, col)
}

func getProvidersFromFlags() []string {
	var providers []string

	if flagAll {
		return cfg.GetEnabledProviders()
	}

	if flagCloudflare {
		providers = append(providers, "cloudflare")
	}
	if flagOracle {
		providers = append(providers, "oracle")
	}
	if flagAzure {
		providers = append(providers, "azure")
	}
	if flagGCP {
		providers = append(providers, "gcp")
	}
	if flagNeon {
		providers = append(providers, "neon")
	}

	// AI provider flags
	switch strings.ToLower(flagAI) {
	case "vast":
		providers = append(providers, "vastai")
	case "io", "runpod":
		providers = append(providers, "runpod")
	case "cf", "cloudflare":
		providers = append(providers, "cloudflare")
	case "oracle":
		providers = append(providers, "oracle")
	}

	// If --provider flag is set
	if flagProvider != "" {
		providers = append(providers, flagProvider)
	}

	return providers
}

func getOutputFormat() string {
	if flagJSON {
		return "json"
	}
	if flagWide {
		return "wide"
	}
	if flagTable {
		return "table"
	}
	return cfg.Defaults.OutputFormat
}

func initializeProviders(ctx context.Context, providerNames []string) (map[string]provider.Provider, error) {
	providers := make(map[string]provider.Provider)

	for _, name := range providerNames {
		// Get provider config
		providerCfg, ok := cfg.Providers[name]
		if !ok {
			// Create minimal config from environment
			providerCfg = config.Provider{
				Enabled: true,
				Auth: config.AuthConfig{
					Method: "env",
				},
			}
		}

		if !providerCfg.Enabled {
			fmt.Fprintf(os.Stderr, "Warning: provider %s is disabled\n", name)
			continue
		}

		// Create provider instance
		p, err := provider.Create(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: provider %s not available: %v\n", name, err)
			continue
		}

		// Convert config to provider config
		pCfg := &provider.ProviderConfig{
			Name:        name,
			Enabled:     providerCfg.Enabled,
			Credentials: providerCfg.Auth.ToCredentials(),
			Options:     providerCfg.Options,
		}

		if providerCfg.RateLimit != nil {
			pCfg.RateLimit = &provider.RateLimitConfig{
				RequestsPerSecond: providerCfg.RateLimit.RequestsPerSecond,
				Burst:             providerCfg.RateLimit.Burst,
				Timeout:           providerCfg.RateLimit.Timeout.Duration(),
			}
		}

		// Initialize provider
		if err := p.Initialize(ctx, pCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize %s: %v\n", name, err)
			continue
		}

		providers[name] = p
	}

	return providers, nil
}

func closeProviders(providers map[string]provider.Provider) {
	for name, p := range providers {
		if err := p.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing provider %s: %v\n", name, err)
		}
	}
}

func runOnce(ctx context.Context, col *collector.Collector) error {
	// Build collection request
	req := buildCollectRequest()

	// Collect data
	resp, err := col.Collect(ctx, req)
	if err != nil {
		return fmt.Errorf("collection failed: %w", err)
	}

	// Format and output results
	formatter := output.NewFormatter(getOutputFormat(), &cfg.Output, os.Stdout)
	return formatter.Format(resp)
}

func runContinuous(ctx context.Context, col *collector.Collector) error {
	ticker := time.NewTicker(flagRefresh)
	defer ticker.Stop()

	for {
		// Clear screen
		fmt.Print("\033[H\033[2J")
		fmt.Printf("cloudtop - refreshing every %v (Ctrl+C to quit)\n", flagRefresh)

		if err := runOnce(ctx, col); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}

		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func runGPUInstances(ctx context.Context, col *collector.Collector) error {
	filter := &provider.GPUFilter{}
	if flagRunning {
		filter.States = []string{"running"}
	}

	instances, errors := col.CollectGPU(ctx, filter)

	// Show errors
	for p, err := range errors {
		fmt.Fprintf(os.Stderr, "Warning: %s: %v\n", p, err)
	}

	// Format output
	formatter := output.NewGPUFormatter(flagWide, os.Stdout)
	return formatter.FormatGPUInstances(instances)
}

func runGPUList(ctx context.Context, col *collector.Collector) error {
	offerings, errors := col.CollectGPUAvailability(ctx)

	// Show errors
	for p, err := range errors {
		fmt.Fprintf(os.Stderr, "Warning: %s: %v\n", p, err)
	}

	// Format output
	formatter := output.NewGPUFormatter(flagWide, os.Stdout)
	return formatter.FormatGPUOfferings(offerings)
}

func buildCollectRequest() *collector.CollectRequest {
	req := &collector.CollectRequest{
		Timeout: 30 * time.Second,
		Filters: &provider.ResourceFilter{},
	}

	// Apply service filter
	if flagService != "" {
		req.Services = []string{flagService}
		req.Filters.Types = []string{flagService}
	}

	// Apply state filter
	if flagRunning {
		req.Filters.Status = []string{"running", "active"}
	}

	return req
}
