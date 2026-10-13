# cloudtop

A comprehensive multi-cloud monitoring CLI tool for viewing load, resources, and metrics across cloud providers.

## Features

- **Multi-Provider Support**: Monitor resources across Cloudflare, Oracle Cloud, Azure, GCP, Neon, and AI/GPU providers
- **GPU Monitoring**: View GPU instances, availability, and pricing from Vast.ai and RunPod
- **Concurrent Fetching**: Parallel API calls for fast data collection
- **Multiple Output Formats**: Table, wide table, and JSON output
- **Auto-Refresh**: Continuous monitoring with configurable refresh intervals
- **Rate Limiting**: Built-in rate limiting to respect API limits
- **Caching**: Reduce API calls with configurable caching

## Installation

```bash
cd cloudtop
go mod tidy
go build -o cloudtop ./cmd/cloudtop
```

## Quick Start

1. Generate a configuration file:
```bash
./cloudtop init
```

2. Edit `cloudtop.json` with your credentials (or set environment variables)

3. View all resources:
```bash
./cloudtop --all
```

## Usage

```bash
# Show all resources from all configured providers
cloudtop --all

# Show only Cloudflare resources
cloudtop --cloudflare
cloudtop -c

# Show Oracle Cloud compute instances
cloudtop --oracle --service compute
cloudtop -o -s compute

# Show Neon databases
cloudtop --neon
cloudtop -n

# Show GPU instances from AI providers
cloudtop --ai vast --gpu
cloudtop --ai io --gpu  # RunPod

# List available GPU compute with pricing
cloudtop --gpu --list

# Show running resources only
cloudtop --all --running

# Output in different formats
cloudtop --all --json       # JSON output
cloudtop --all --wide       # Wide table with more columns
cloudtop --all --table      # Standard table (default)

# Auto-refresh every 30 seconds
cloudtop --all --refresh 30s

# Filter by provider
cloudtop --provider vastai --running
```

## Providers

### Cloud Providers
| Flag | Provider | Description |
|------|----------|-------------|
| `-c, --cloudflare` | Cloudflare | Workers, R2, D1, KV, AI |
| `-o, --oracle` | Oracle Cloud | Compute, OKE, Autonomous DB |
| `--azure` | Azure | VMs, AKS, Functions (stub) |
| `-g, --gcp` | GCP | Compute Engine, GKE (stub) |
| `-n, --neon` | Neon | Serverless Postgres |

### AI/GPU Providers
| Flag | Provider | Description |
|------|----------|-------------|
| `--ai vast` | Vast.ai | GPU rental marketplace |
| `--ai io` | RunPod | Serverless GPU compute |
| `--ai cf` | Cloudflare AI | Inference API |
| `--ai oracle` | Oracle GPU | A100, V100 instances |

## Configuration

Copy `cloudtop.json.example` to `cloudtop.json` and configure your providers:

```json
{
  "providers": {
    "cloudflare": {
      "enabled": true,
      "auth": {
        "method": "api_key",
        "env_api_key": "CLOUDFLARE_API_TOKEN"
      },
      "options": {
        "account_id": "your-account-id"
      }
    }
  }
}
```

### Environment Variables

| Variable | Provider |
|----------|----------|
| `CLOUDFLARE_API_TOKEN` | Cloudflare |
| `NEON_API_KEY` | Neon |
| `VASTAI_API_KEY` | Vast.ai |
| `RUNPOD_API_KEY` | RunPod |

### Oracle Cloud

Uses `~/.oci/config` file format (standard OCI SDK configuration).

## Cost Tracking

cloudtop includes built-in cost tracking for Oracle Cloud resources with support for multiple spend tracking modes:

### Spend Tracking Flags

```bash
# Show year-to-date spend (from Jan 1 to current day)
cloudtop --all --ytd

# Show day-of-month spend (from start of month to current day)
cloudtop --all --dom

# Show last month's total spend
cloudtop --all --last-month
cloudtop --all --lm

# Estimate full month spend based on current usage
cloudtop --all --estimate-month
cloudtop --all --em

# Combine multiple spend tracking options
cloudtop --all --ytd --dom --estimate-month
```

### Pricing Information

- Pricing based on public OCI pricing for commercial regions
- Supports Standard, AMD, GPU, Bare Metal, and ARM shapes
- Free tier resources (E2.1.Micro, A1.Flex) correctly priced at $0.00
- Cost calculations account for actual resource runtime from creation time

### Example Output

```
=== ORACLE - IAD ========================================
  Resources: 16 | Est. Monthly Cost: $304.20

======================================================================
  CROSS-REGION SUMMARY
======================================================================

  Totals:
    Resources: 16 across 1 regions (1 providers)
    Est. Total Monthly Cost: $304.20
    Year-to-Date Spend: $100.05
    Day-of-Month Spend: $100.05
    Estimated Full Month: $258.47
```

## Architecture

```
cloudtop/
├── cmd/cloudtop/          # CLI entry point
├── internal/
│   ├── provider/          # Provider implementations
│   │   ├── cloudflare/    # Cloudflare Workers, R2, D1
│   │   ├── oracle/        # OCI Compute, OKE
│   │   ├── neon/          # Serverless Postgres
│   │   ├── vastai/        # GPU marketplace
│   │   ├── runpod/        # Serverless GPU
│   │   ├── azure/         # Azure (stub)
│   │   └── gcp/           # GCP (stub)
│   ├── collector/         # Concurrent data collection
│   ├── output/            # Table/JSON formatters
│   ├── config/            # Configuration management
│   ├── errors/            # Error types
│   └── metrics/           # Metric types
└── pkg/
    ├── ratelimit/         # Token bucket rate limiting
    └── retry/             # Exponential backoff
```

## Output Examples

### Standard Table
```
=== CLOUDFLARE ==================================================
NAME                          TYPE            REGION          STATUS
------------------------------  --------------  --------------  ----------
my-worker                     workers         global          active
my-bucket                     r2              global          active

Completed in 823ms
```

### GPU Availability (--gpu --list)
```
PROVIDER    GPU TYPE            GPU   MEM       AVAIL   $/HR
----------  ------------------  ----  --------  ------  --------
vastai      RTX 4090            1     24GB      Yes     $0.35
runpod      A100 80GB           1     80GB      Yes     $1.89
vastai      A6000               1     48GB      Yes     $0.42
```

## License

MIT
