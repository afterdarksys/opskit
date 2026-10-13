package cmd

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/afterdarksys/opskit/statsagent/output"
)

// NewPushCommand returns the `push` subcommand.
func NewPushCommand() *cobra.Command {
	cfg := DefaultConfig()
	var endpoint string
	var interval string

	cmd := &cobra.Command{
		Use:   "push [endpoint]",
		Short: "Collect metrics and push JSON to a remote endpoint on an interval",
		Long: `Collect metrics every INTERVAL and POST them as JSON to ENDPOINT.

The endpoint receives a JSON body identical to GET /stats.

Examples:
  statsagent push https://collector.example.com/ingest
  statsagent push --interval 60s https://collector.example.com/ingest`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				endpoint = args[0]
			} else if cfg.PushEndpoint != "" {
				endpoint = cfg.PushEndpoint
			} else {
				return fmt.Errorf("endpoint required (arg or $STATSAGENT_PUSH_ENDPOINT)")
			}
			if interval != "" {
				d, err := time.ParseDuration(interval)
				if err != nil {
					return fmt.Errorf("invalid interval: %w", err)
				}
				cfg.PushInterval = d
			}
			return runPush(cfg, endpoint)
		},
	}

	cmd.Flags().StringVar(&interval, "interval", "", "Push interval (e.g. 30s, 1m)")

	return cmd
}

func runPush(cfg *Config, endpoint string) error {
	auth := "no"
	if cfg.APIKey != "" {
		auth = "yes"
	}
	log.Printf("statsagent push  endpoint=%s  interval=%s  authenticated=%s", endpoint, cfg.PushInterval, auth)

	// Prime collectors
	collect(cfg)

	ticker := time.NewTicker(cfg.PushInterval)
	defer ticker.Stop()

	for {
		snap := collect(cfg)
		if err := pushSnapshot(cfg, endpoint, snap); err != nil {
			log.Printf("push error: %v", err)
		} else {
			log.Printf("pushed snapshot ts=%s", snap.Timestamp.Format(time.RFC3339))
		}
		<-ticker.C
	}
}

// pushClient bounds every push. The previous implementation used http.Post,
// which uses http.DefaultClient and has NO timeout -- a collector that accepted
// the connection but never responded would block the push loop forever, and the
// agent would silently stop reporting.
var pushClient = &http.Client{Timeout: 15 * time.Second}

// warnedPlaintext ensures the plaintext-credential warning is logged once per
// process rather than on every push.
var warnedPlaintext bool

// warnIfPlaintextCredential complains when an API key would be sent over an
// unencrypted connection to a non-local host. It warns rather than refuses:
// pushing to an in-cluster endpoint such as http://metrics-api:3002 over a
// private Docker network is legitimate, and refusing would break it.
func warnIfPlaintextCredential(endpoint string) {
	if warnedPlaintext {
		return
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" {
		return
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return
	}
	log.Printf("WARNING: sending an API key over plaintext http to %q. Use https for any endpoint outside a trusted network.", host)
	warnedPlaintext = true
}

func pushSnapshot(cfg *Config, endpoint string, snap *output.StatsSnapshot) error {
	var buf bytes.Buffer
	if err := output.WriteJSONCompact(&buf, snap); err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, &buf)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Authenticate when configured. Absence is not an error: a collector that
	// does not require auth stays usable, and the server rejects us if it does.
	if cfg != nil && cfg.APIKey != "" {
		warnIfPlaintextCredential(endpoint)
		req.Header.Set("X-API-Key", cfg.APIKey)
	}

	resp, err := pushClient.Do(req)
	if err != nil {
		// The error may embed the URL but never the header, so the key cannot
		// leak through this path.
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused rather than abandoned per push.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("server returned %d: check STATSAGENT_API_KEY", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}
