package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/afterdarksys/opskit/statsagent/cmd"
)

var version = "1.0.0"

func main() {
	root := &cobra.Command{
		Use:     "statsagent",
		Short:   "Lightweight stats collection daemon for hosts, Docker, and k3s",
		Version: version,
		Long: `statsagent collects system, disk, network, process, Docker, and k3s metrics.
It runs on bare metal, inside Docker containers, or as a k3s DaemonSet.
Targets: Rocky Linux, Debian, Ubuntu.

Commands:
  serve    Start HTTP server exposing /metrics (Prometheus) and /stats (JSON)
  push     Collect once and push JSON to a remote endpoint
  once     Collect once, print to stdout, and exit
  install  Install as a systemd service`,
	}

	root.AddCommand(cmd.NewServeCommand())
	root.AddCommand(cmd.NewPushCommand())
	root.AddCommand(cmd.NewOnceCommand())
	root.AddCommand(cmd.NewInstallCommand())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
