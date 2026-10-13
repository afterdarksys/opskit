// Package cmd contains all CLI subcommands for statsagent.
package cmd

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for statsagent.
type Config struct {
	Port         int
	Interval     time.Duration
	PushEndpoint string
	// APIKey authenticates pushes to the ADS control plane. Sent as the
	// X-API-Key header; never logged.
	APIKey        string
	PushInterval  time.Duration
	Labels        map[string]string
	DockerSocket  string
	K3sKubeconfig string
	TopN          int
	EnableDocker  bool
	EnableK3s     bool
	EnableProcess bool
}

// DefaultConfig returns configuration from environment variables with sensible defaults.
func DefaultConfig() *Config {
	c := &Config{
		Port:          9100,
		Interval:      15 * time.Second,
		PushInterval:  30 * time.Second,
		Labels:        make(map[string]string),
		DockerSocket:  "/var/run/docker.sock",
		K3sKubeconfig: "/etc/rancher/k3s/k3s.yaml",
		TopN:          10,
		EnableDocker:  true,
		EnableK3s:     true,
		EnableProcess: true,
	}

	if v := os.Getenv("STATSAGENT_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Port = port
		}
	}
	if v := os.Getenv("STATSAGENT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.Interval = d
		}
	}
	if v := os.Getenv("STATSAGENT_PUSH_ENDPOINT"); v != "" {
		c.PushEndpoint = v
	}
	// API key. The _FILE variant takes precedence: it is the safer way to hand a
	// secret to a container (Docker/Kubernetes secrets land on disk, not in the
	// environment, where they would be visible to `docker inspect` and to any
	// child process). Whitespace is trimmed because files usually end in a newline.
	if v := os.Getenv("STATSAGENT_API_KEY_FILE"); v != "" {
		if data, err := os.ReadFile(v); err == nil {
			c.APIKey = strings.TrimSpace(string(data))
		}
		// A read failure is deliberately silent here: config loading has no logger
		// and must not print anything that hints at the secret's location. push
		// reports the resulting "no API key" condition instead.
	}
	if c.APIKey == "" {
		if v := os.Getenv("STATSAGENT_API_KEY"); v != "" {
			c.APIKey = strings.TrimSpace(v)
		}
	}
	if v := os.Getenv("STATSAGENT_PUSH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.PushInterval = d
		}
	}
	if v := os.Getenv("STATSAGENT_DOCKER_SOCKET"); v != "" {
		c.DockerSocket = v
	}
	if v := os.Getenv("STATSAGENT_K3S_KUBECONFIG"); v != "" {
		c.K3sKubeconfig = v
	}
	if v := os.Getenv("STATSAGENT_TOP_N"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.TopN = n
		}
	}

	// STATSAGENT_LABELS=dc=iad1,env=prod
	if v := os.Getenv("STATSAGENT_LABELS"); v != "" {
		for _, pair := range strings.Split(v, ",") {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				c.Labels[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	return c
}

// detectContext determines whether we're running on a bare host, inside Docker, or k3s.
func detectContext() string {
	// k3s / Kubernetes
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return "k3s"
	}
	// Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	// Check cgroup for docker/containerd
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		if strings.Contains(s, "docker") || strings.Contains(s, "containerd") {
			return "docker"
		}
	}
	return "host"
}
