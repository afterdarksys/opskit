package output

import (
	"encoding/json"
	"io"
	"time"

	"github.com/afterdarksys/opskit/statsagent/collectors"
)

// StatsSnapshot is the full JSON payload from one collection cycle.
type StatsSnapshot struct {
	Timestamp time.Time                `json:"timestamp"`
	Context   string                   `json:"context"`  // host, docker, k3s
	System    *collectors.SystemStats  `json:"system,omitempty"`
	Disk      *collectors.DiskStats    `json:"disk,omitempty"`
	Network   *collectors.NetworkStats `json:"network,omitempty"`
	Process   *collectors.ProcessStats `json:"process,omitempty"`
	Docker    *collectors.DockerStats  `json:"docker,omitempty"`
	K3s       *collectors.K3sStats     `json:"k3s,omitempty"`
}

// WriteJSON encodes the snapshot as indented JSON to w.
func WriteJSON(w io.Writer, snap *StatsSnapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

// WriteJSONCompact encodes the snapshot without indentation (for push payloads).
func WriteJSONCompact(w io.Writer, snap *StatsSnapshot) error {
	return json.NewEncoder(w).Encode(snap)
}
