# opskit

Two small, independent Go command-line tools, `statsagent` and `cloudtop`,
for people who run their own servers. Each lives in its own Go module and
builds on its own; nothing here
needs a database, a control plane or a particular cloud account.

| Tool | Path | What it does |
|------|------|--------------|
| `statsagent` | `statsagent/` | Collects host, disk, network, process, Docker and k3s metrics. Serves them as Prometheus text and JSON, prints them once, or pushes JSON to an HTTP endpoint. |
| `cloudtop` | `cloudtop/` | A `top`-style CLI that lists resources, GPU offers and estimated cost across Cloudflare, Oracle Cloud, Neon, Vast.ai and RunPod (Azure and GCP are stubs). |

Status: working tools used on a small Linux fleet, not a product. There is no
compliance claim of any kind. Only `statsagent` has automated tests;
`cloudtop` builds but has none yet.

## Build and test

Go 1.21 or newer. Install either tool directly:

```sh
go install github.com/afterdarksys/opskit/statsagent@latest
go install github.com/afterdarksys/opskit/cloudtop/cmd/cloudtop@latest
```

From a checkout, each tool is its own module (`github.com/afterdarksys/opskit/statsagent`
and `github.com/afterdarksys/opskit/cloudtop`); the root `go.work` ties them
together, so from the repository root:

```sh
go build ./statsagent/... ./cloudtop/...
go test ./statsagent/... ./cloudtop/...
```

or inside one tool's directory:

```sh
cd statsagent && go build -o statsagent .
cd cloudtop && go build -o cloudtop ./cmd/cloudtop
```

## statsagent

```sh
statsagent once                 # collect once, print JSON, exit
statsagent serve                # HTTP server: /metrics /stats /health /ready
statsagent push https://collector.example.com/ingest
statsagent install --port 9100  # write and enable a systemd unit (run as root)
```

Configuration is by environment: `STATSAGENT_PORT` (9100),
`STATSAGENT_INTERVAL`, `STATSAGENT_LABELS=dc=x,env=prod`,
`STATSAGENT_DOCKER_SOCKET`, `STATSAGENT_K3S_KUBECONFIG`, `STATSAGENT_TOP_N`,
and for `push`: `STATSAGENT_PUSH_ENDPOINT`, `STATSAGENT_PUSH_INTERVAL`,
`STATSAGENT_API_KEY_FILE` or `STATSAGENT_API_KEY` (sent as `X-API-Key`).

Things to know before you run it:

- **`serve` listens on every interface, without authentication.** It binds
  `:<port>` (default 9100) so a Prometheus server on another machine can
  scrape it. `/metrics` and `/stats` reveal hostnames, process names, mount
  points, container names and k3s namespaces. Firewall the port to your
  scraper, or run it only on a private network.
- `push` sends the API key over whatever scheme the endpoint uses and logs a
  warning, once, when that is plain `http://` to a non-local host. Use
  `https://`.
- The k3s collector always verifies the API server's certificate, against
  the CA in the kubeconfig (`certificate-authority` or
  `certificate-authority-data`, which k3s writes into `k3s.yaml`) or, when
  there is none, against the system roots. A kubeconfig without a usable CA
  for a self-signed API server makes the collector fail rather than skip
  verification. TLS 1.3 is the minimum.
- `deploy/` has a Dockerfile, a systemd unit and an install script.
  `make docker IMAGE=your/registry/statsagent` tags the image.

## cloudtop

```sh
cloudtop init                   # write a starter cloudtop.json
cloudtop --all                  # every configured provider
cloudtop --gpu --list           # GPU offers with prices
cloudtop --all --json
```

Credentials come from environment variables named in `cloudtop.json`
(`CLOUDFLARE_API_TOKEN`, `NEON_API_KEY`, `VASTAI_API_KEY`, `RUNPOD_API_KEY`)
and, for Oracle Cloud, from `~/.oci/config`. Never commit a filled-in
`cloudtop.json`; start from `cloudtop/cloudtop.json.example`. Cost figures
are estimates from public list prices, not invoices. See `cloudtop/README.md`.

## License and security

MIT, see `LICENSE`. To report a vulnerability, see `SECURITY.md`.
