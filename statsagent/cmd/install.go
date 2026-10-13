package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// NewInstallCommand returns the `install` subcommand.
func NewInstallCommand() *cobra.Command {
	var binPath string
	var serviceName string
	var port int
	var interval string
	var uninstall bool
	var labels string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install statsagent as a systemd service (Rocky Linux, Debian, Ubuntu)",
		Long: `Install statsagent as a systemd service.

Requires root (run with sudo).

Examples:
  sudo statsagent install
  sudo statsagent install --port 9100 --interval 15s
  sudo statsagent install --uninstall`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("install is only supported on Linux (detected: %s)", runtime.GOOS)
			}
			if os.Geteuid() != 0 {
				return fmt.Errorf("install requires root — run with sudo")
			}
			if uninstall {
				return runUninstall(serviceName)
			}
			return runInstall(binPath, serviceName, port, interval, labels)
		},
	}

	defaultBin, _ := os.Executable()
	cmd.Flags().StringVar(&binPath, "bin", defaultBin, "Path to statsagent binary to install")
	cmd.Flags().StringVar(&serviceName, "service-name", "statsagent", "Systemd service name")
	cmd.Flags().IntVar(&port, "port", 9100, "Listen port")
	cmd.Flags().StringVar(&interval, "interval", "15s", "Collection interval")
	cmd.Flags().StringVar(&labels, "labels", "", "Extra labels (key=val,key=val)")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "Remove the service")

	return cmd
}

func runInstall(srcBin, serviceName string, port int, interval, labels string) error {
	destBin := "/usr/local/bin/" + serviceName
	unitPath := "/etc/systemd/system/" + serviceName + ".service"

	fmt.Printf("Installing %s → %s\n", srcBin, destBin)

	// Copy binary
	if srcBin != destBin {
		if err := copyFile(srcBin, destBin); err != nil {
			return fmt.Errorf("copy binary: %w", err)
		}
		if err := os.Chmod(destBin, 0755); err != nil {
			return fmt.Errorf("chmod binary: %w", err)
		}
	}

	// Detect distro for any distro-specific notes
	distro := detectDistro()
	fmt.Printf("Detected distro: %s\n", distro)

	// Build environment block
	envBlock := fmt.Sprintf("Environment=STATSAGENT_PORT=%d\nEnvironment=STATSAGENT_INTERVAL=%s", port, interval)
	if labels != "" {
		envBlock += fmt.Sprintf("\nEnvironment=STATSAGENT_LABELS=%s", labels)
	}

	// Write systemd unit file (works on Rocky/Debian/Ubuntu — all use systemd)
	unit := fmt.Sprintf(`[Unit]
Description=StatsAgent - Lightweight System Stats Collector
After=network.target
Wants=network.target

[Service]
Type=simple
User=root
ExecStart=%s serve
Restart=on-failure
RestartSec=5s
%s

# Harden the service slightly
ProtectSystem=full
ReadWritePaths=/tmp

[Install]
WantedBy=multi-user.target
`, destBin, envBlock)

	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	fmt.Printf("Wrote unit: %s\n", unitPath)

	// Reload and enable
	for _, args := range [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", serviceName},
		{"systemctl", "restart", serviceName},
	} {
		if err := runCmd(args...); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
		}
	}

	fmt.Printf("\n✓ %s installed and started\n", serviceName)
	fmt.Printf("  Status:  systemctl status %s\n", serviceName)
	fmt.Printf("  Logs:    journalctl -u %s -f\n", serviceName)
	fmt.Printf("  Metrics: curl http://localhost:%d/metrics\n", port)
	fmt.Printf("  JSON:    curl http://localhost:%d/stats\n", port)

	return nil
}

func runUninstall(serviceName string) error {
	fmt.Printf("Uninstalling %s...\n", serviceName)

	unitPath := "/etc/systemd/system/" + serviceName + ".service"
	destBin := "/usr/local/bin/" + serviceName

	for _, args := range [][]string{
		{"systemctl", "stop", serviceName},
		{"systemctl", "disable", serviceName},
	} {
		_ = runCmd(args...) // ignore errors — service may not be running
	}

	for _, path := range []string{unitPath, destBin} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Printf("  Warning: could not remove %s: %v\n", path, err)
		} else {
			fmt.Printf("  Removed: %s\n", path)
		}
	}

	_ = runCmd("systemctl", "daemon-reload")
	fmt.Printf("✓ %s uninstalled\n", serviceName)
	return nil
}

func detectDistro() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			id := strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID="), `"`))
			switch id {
			case "rocky":
				return "rocky"
			case "debian":
				return "debian"
			case "ubuntu":
				return "ubuntu"
			case "rhel", "centos", "almalinux":
				return "rhel-like"
			default:
				return id
			}
		}
	}
	return "unknown"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Ensure dest directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 1<<20)
	for {
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
		}
		if readErr != nil {
			break
		}
	}
	return nil
}

func runCmd(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
