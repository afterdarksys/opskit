package cmd

import (
	"log"
	"time"

	"github.com/afterdarksys/opskit/statsagent/collectors"
	"github.com/afterdarksys/opskit/statsagent/output"
)

// collect runs all enabled collectors and returns a snapshot.
// The first call primes the delta-based collectors; metrics will be near-zero on the first sample.
func collect(cfg *Config) *output.StatsSnapshot {
	snap := &output.StatsSnapshot{
		Timestamp: time.Now(),
		Context:   detectContext(),
	}

	if sys, err := collectors.CollectSystem(); err != nil {
		log.Printf("system collector error: %v", err)
	} else {
		snap.System = sys
	}

	if disk, err := collectors.CollectDisk(); err != nil {
		log.Printf("disk collector error: %v", err)
	} else {
		snap.Disk = disk
	}

	if net, err := collectors.CollectNetwork(); err != nil {
		log.Printf("network collector error: %v", err)
	} else {
		snap.Network = net
	}

	if cfg.EnableProcess {
		if proc, err := collectors.CollectProcess(cfg.TopN); err != nil {
			log.Printf("process collector error: %v", err)
		} else {
			snap.Process = proc
		}
	}

	if cfg.EnableDocker {
		if docker, err := collectors.CollectDocker(cfg.DockerSocket); err != nil {
			log.Printf("docker collector error: %v", err)
		} else {
			snap.Docker = docker
		}
	}

	if cfg.EnableK3s {
		if k3s, err := collectors.CollectK3s(cfg.K3sKubeconfig); err != nil {
			log.Printf("k3s collector error: %v", err)
		} else {
			snap.K3s = k3s
		}
	}

	return snap
}
