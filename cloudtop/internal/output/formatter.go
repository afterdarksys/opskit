package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/afterdarksys/opskit/cloudtop/internal/config"
	"github.com/afterdarksys/opskit/cloudtop/internal/provider"
)

// Formatter interface for output formatting
type Formatter interface {
	Format(result *CollectResult) error
}

// CollectResult contains aggregated results from all providers
type CollectResult struct {
	Results   map[string]*ProviderResult
	Errors    map[string]error
	Timestamp time.Time
	Duration  time.Duration
}

// ProviderResult contains results from a single provider
type ProviderResult struct {
	Provider  string
	Resources []provider.Resource
	Metrics   map[string]interface{}
	Cached    bool
	Duration  time.Duration
}

// NewFormatter creates a new formatter based on format type
func NewFormatter(format string, cfg *config.OutputConfig, w io.Writer) Formatter {
	if w == nil {
		w = os.Stdout
	}

	switch format {
	case "json":
		return &JSONFormatter{writer: w}
	case "wide":
		return &TableFormatter{writer: w, config: cfg, wide: true}
	default:
		return &TableFormatter{writer: w, config: cfg, wide: false}
	}
}

// TableFormatter outputs results as ASCII table
type TableFormatter struct {
	writer io.Writer
	config *config.OutputConfig
	wide   bool
}

func (f *TableFormatter) Format(result *CollectResult) error {
	// Group resources by provider
	providers := make([]string, 0, len(result.Results))
	for p := range result.Results {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	for _, providerName := range providers {
		provResult := result.Results[providerName]
		if len(provResult.Resources) == 0 {
			continue
		}

		// Print provider header
		fmt.Fprintf(f.writer, "\n%s %s %s\n",
			strings.Repeat("=", 3),
			strings.ToUpper(providerName),
			strings.Repeat("=", 50-len(providerName)))

		// Determine columns based on format
		var headers []string
		var widths []int
		if f.wide {
			headers = []string{"ID", "NAME", "TYPE", "REGION", "STATUS", "CREATED"}
			widths = []int{20, 25, 15, 15, 10, 20}
		} else {
			headers = []string{"NAME", "TYPE", "REGION", "STATUS"}
			widths = []int{30, 15, 15, 10}
		}

		// Print headers
		f.printRow(headers, widths)
		f.printSeparator(widths)

		// Print resources
		for _, resource := range provResult.Resources {
			var row []string
			if f.wide {
				created := ""
				if !resource.CreatedAt.IsZero() {
					created = resource.CreatedAt.Format("2006-01-02 15:04")
				}
				row = []string{
					truncate(resource.ID, widths[0]),
					truncate(resource.Name, widths[1]),
					resource.Type,
					resource.Region,
					resource.Status,
					created,
				}
			} else {
				row = []string{
					truncate(resource.Name, widths[0]),
					resource.Type,
					resource.Region,
					resource.Status,
				}
			}
			f.printRow(row, widths)
		}

		if provResult.Cached {
			fmt.Fprintf(f.writer, "(cached)\n")
		}
	}

	// Print errors
	if len(result.Errors) > 0 {
		fmt.Fprintf(f.writer, "\nErrors:\n")
		for p, err := range result.Errors {
			fmt.Fprintf(f.writer, "  %s: %v\n", p, err)
		}
	}

	// Print summary
	fmt.Fprintf(f.writer, "\nCompleted in %v\n", result.Duration.Round(time.Millisecond))

	return nil
}

func (f *TableFormatter) printRow(columns []string, widths []int) {
	for i, col := range columns {
		format := fmt.Sprintf("%%-%ds  ", widths[i])
		fmt.Fprintf(f.writer, format, col)
	}
	fmt.Fprintln(f.writer)
}

func (f *TableFormatter) printSeparator(widths []int) {
	for i, w := range widths {
		fmt.Fprint(f.writer, strings.Repeat("-", w))
		if i < len(widths)-1 {
			fmt.Fprint(f.writer, "  ")
		}
	}
	fmt.Fprintln(f.writer)
}

// JSONFormatter outputs results as JSON
type JSONFormatter struct {
	writer io.Writer
}

func (f *JSONFormatter) Format(result *CollectResult) error {
	output := struct {
		Timestamp time.Time                     `json:"timestamp"`
		Duration  string                        `json:"duration"`
		Providers map[string]*ProviderResult    `json:"providers"`
		Errors    map[string]string             `json:"errors,omitempty"`
	}{
		Timestamp: result.Timestamp,
		Duration:  result.Duration.String(),
		Providers: result.Results,
		Errors:    make(map[string]string),
	}

	for p, err := range result.Errors {
		output.Errors[p] = err.Error()
	}

	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// GPUFormatter outputs GPU-specific results
type GPUFormatter struct {
	writer io.Writer
	wide   bool
}

func NewGPUFormatter(wide bool, w io.Writer) *GPUFormatter {
	if w == nil {
		w = os.Stdout
	}
	return &GPUFormatter{writer: w, wide: wide}
}

func (f *GPUFormatter) FormatGPUInstances(instances []provider.GPUInstance) error {
	if len(instances) == 0 {
		fmt.Fprintln(f.writer, "No GPU instances found")
		return nil
	}

	var headers []string
	var widths []int
	if f.wide {
		headers = []string{"PROVIDER", "NAME", "GPU TYPE", "GPU COUNT", "GPU MEM", "CPU", "RAM", "STATUS", "$/HR"}
		widths = []int{10, 20, 15, 9, 8, 5, 8, 10, 8}
	} else {
		headers = []string{"PROVIDER", "NAME", "GPU TYPE", "GPU", "STATUS", "$/HR"}
		widths = []int{10, 20, 15, 4, 10, 8}
	}

	f.printRow(headers, widths)
	f.printSeparator(widths)

	for _, inst := range instances {
		var row []string
		if f.wide {
			row = []string{
				inst.Provider,
				truncate(inst.Name, widths[1]),
				inst.GPUType,
				fmt.Sprintf("%d", inst.GPUCount),
				fmt.Sprintf("%.0fGB", inst.GPUMemoryGB),
				fmt.Sprintf("%d", inst.CPUCores),
				fmt.Sprintf("%.0fGB", inst.MemoryGB),
				inst.Status,
				fmt.Sprintf("$%.2f", inst.PricePerHour),
			}
		} else {
			row = []string{
				inst.Provider,
				truncate(inst.Name, widths[1]),
				inst.GPUType,
				fmt.Sprintf("%d", inst.GPUCount),
				inst.Status,
				fmt.Sprintf("$%.2f", inst.PricePerHour),
			}
		}
		f.printRow(row, widths)
	}

	return nil
}

func (f *GPUFormatter) FormatGPUOfferings(offerings []provider.GPUOffering) error {
	if len(offerings) == 0 {
		fmt.Fprintln(f.writer, "No GPU offerings found")
		return nil
	}

	// Sort by price
	sort.Slice(offerings, func(i, j int) bool {
		return offerings[i].PricePerHour < offerings[j].PricePerHour
	})

	var headers []string
	var widths []int
	if f.wide {
		headers = []string{"PROVIDER", "GPU TYPE", "GPU", "GPU MEM", "CPU", "RAM", "REGION", "AVAIL", "$/HR"}
		widths = []int{10, 18, 4, 8, 5, 8, 15, 6, 8}
	} else {
		headers = []string{"PROVIDER", "GPU TYPE", "GPU", "MEM", "AVAIL", "$/HR"}
		widths = []int{10, 18, 4, 8, 6, 8}
	}

	f.printRow(headers, widths)
	f.printSeparator(widths)

	for _, offer := range offerings {
		avail := "No"
		if offer.Available {
			avail = "Yes"
		}

		var row []string
		if f.wide {
			row = []string{
				offer.Provider,
				offer.GPUType,
				fmt.Sprintf("%d", offer.GPUCount),
				fmt.Sprintf("%.0fGB", offer.GPUMemoryGB),
				fmt.Sprintf("%d", offer.CPUCores),
				fmt.Sprintf("%.0fGB", offer.MemoryGB),
				truncate(offer.Region, widths[6]),
				avail,
				fmt.Sprintf("$%.2f", offer.PricePerHour),
			}
		} else {
			row = []string{
				offer.Provider,
				offer.GPUType,
				fmt.Sprintf("%d", offer.GPUCount),
				fmt.Sprintf("%.0fGB", offer.GPUMemoryGB),
				avail,
				fmt.Sprintf("$%.2f", offer.PricePerHour),
			}
		}
		f.printRow(row, widths)
	}

	return nil
}

func (f *GPUFormatter) printRow(columns []string, widths []int) {
	for i, col := range columns {
		format := fmt.Sprintf("%%-%ds  ", widths[i])
		fmt.Fprintf(f.writer, format, col)
	}
	fmt.Fprintln(f.writer)
}

func (f *GPUFormatter) printSeparator(widths []int) {
	for i, w := range widths {
		fmt.Fprint(f.writer, strings.Repeat("-", w))
		if i < len(widths)-1 {
			fmt.Fprint(f.writer, "  ")
		}
	}
	fmt.Fprintln(f.writer)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
