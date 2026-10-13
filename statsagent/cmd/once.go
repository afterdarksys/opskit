package cmd

import (
	"bytes"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/afterdarksys/opskit/statsagent/output"
)

// NewOnceCommand returns the `once` subcommand.
func NewOnceCommand() *cobra.Command {
	cfg := DefaultConfig()
	var format string

	cmd := &cobra.Command{
		Use:   "once",
		Short: "Collect metrics once, print to stdout, and exit",
		Long: `Collect a single metrics snapshot and print to stdout.

Useful for testing, scripting, or one-shot cron jobs.

Examples:
  statsagent once
  statsagent once --format prometheus
  statsagent once --format json | jq .system.cpu_used_pct`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnce(cfg, format)
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "Output format: json or prometheus")

	return cmd
}

func runOnce(cfg *Config, format string) error {
	// Prime delta collectors (discarded)
	collect(cfg)
	// Second sample has real deltas
	snap := collect(cfg)

	switch format {
	case "prometheus":
		pw := output.NewPrometheusWriter(cfg.Labels)
		var buf bytes.Buffer
		pw.WriteAll(&buf, snap.System, snap.Disk, snap.Network, snap.Process, snap.Docker, snap.K3s)
		fmt.Print(buf.String())
	default:
		return output.WriteJSON(os.Stdout, snap)
	}

	return nil
}
