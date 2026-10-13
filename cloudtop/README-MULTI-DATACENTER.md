# Multi-Datacenter Support in CloudTop

CloudTop now supports comprehensive multi-datacenter/multi-region infrastructure monitoring with flexible configuration and display modes.

## Features

### Per-Provider Region Control
Each provider can be configured to query all regions or just a primary region:

```json
{
  "providers": {
    "oracle": {
      "enabled": true,
      "query_all_regions": true,
      "regions": ["us-ashburn-1", "us-phoenix-1", "us-sanjose-1"],
      ...
    }
  }
}
```

### Configuration Options

#### Provider-Level Settings

- `query_all_regions` (bool): Whether to query all configured regions by default
- `regions` ([]string): List of regions/datacenters to query

#### Global Defaults

```json
{
  "defaults": {
    "region_display_mode": "grouped",
    "show_region_stats": true,
    "show_total_aggregates": true
  }
}
```

- `region_display_mode`: How to display multi-region results ("mixed", "grouped", "separate")
- `show_region_stats`: Show per-region statistics
- `show_total_aggregates`: Show cross-region aggregated totals

## CLI Usage

### Region Filtering

```bash
# Query specific regions
cloudtop --oracle --regions us-ashburn-1,us-phoenix-1

# Query all configured regions (override per-provider settings)
cloudtop --oracle --all-regions

# Filter by region using short flag
cloudtop --all -r us-ashburn-1
```

### Display Modes

```bash
# Mixed view: All resources in one list with region column
cloudtop --all --region-display mixed

# Grouped view (default): Resources grouped by region with headers
cloudtop --all --region-display grouped

# Separate view: Each region in its own detailed section
cloudtop --all --region-display separate
```

### Combined Queries

```bash
# Show all Oracle resources across all regions in grouped view
cloudtop --oracle --all-regions --region-display grouped

# Show only running instances in specific regions
cloudtop --oracle --running --regions us-ashburn-1,us-phoenix-1

# Wide format with separate sections per region
cloudtop --all --wide --region-display separate
```

## Display Modes Explained

### Mixed Mode
```
=== ALL RESOURCES (MIXED VIEW) ===

PROVIDER      REGION            NAME                  TYPE       STATUS
--------      ------            ----                  ----       ------
oracle        us-ashburn-1      web-server-1         compute    running
oracle        us-ashburn-1      db-primary           compute    running
oracle        us-phoenix-1      web-server-2         compute    running
oracle        us-phoenix-1      cache-server         compute    stopped

=== CROSS-REGION SUMMARY ===
Total: 4 resources across 2 regions
```

### Grouped Mode (Default)
```
=== ORACLE - US-ASHBURN-1 ===
Resources: 2 | Est. Monthly Cost: $150.00

NAME              TYPE       STATUS
----              ----       ------
web-server-1      compute    running
db-primary        compute    running

=== ORACLE - US-PHOENIX-1 ===
Resources: 2 | Est. Monthly Cost: $120.00

NAME              TYPE       STATUS
----              ----       ------
web-server-2      compute    running
cache-server      compute    stopped

=== CROSS-REGION SUMMARY ===
Total: 4 resources across 2 regions
Est. Total Monthly Cost: $270.00
```

### Separate Mode
```
======================================================================
  REGION: us-ashburn-1 (oracle)
======================================================================

  Statistics:
    Total Resources: 2
    By Type:
      compute              : 2
    By Status:
      running              : 2
    Est. Monthly Cost: $150.00

NAME              TYPE       STATUS
----              ----       ------
web-server-1      compute    running
db-primary        compute    running

======================================================================
  REGION: us-phoenix-1 (oracle)
======================================================================

  Statistics:
    Total Resources: 2
    By Type:
      compute              : 2
    By Status:
      running              : 1
      stopped              : 1
    Est. Monthly Cost: $120.00

NAME              TYPE       STATUS
----              ----       ------
web-server-2      compute    running
cache-server      compute    stopped
```

## Cross-Region Aggregated Metrics

When `show_total_aggregates` is enabled, you get comprehensive statistics:

```
=== CROSS-REGION SUMMARY ===

  Totals:
    Resources: 15 across 3 regions (2 providers)
    Est. Total Monthly Cost: $1,245.00

  By Provider:
    oracle              : 12 resources
    cloudflare          : 3 resources

  By Region:
    us-ashburn-1        : 8 resources
    us-phoenix-1        : 5 resources
    us-sanjose-1        : 2 resources

  By Type:
    compute             : 10
    database            : 3
    storage             : 2

  By Status:
    running             : 13
    stopped             : 2
```

## Examples

### Monitor Expanded Infrastructure

```bash
# See all resources across all datacenters
cloudtop --all --all-regions

# Focus on production regions only
cloudtop --oracle --regions us-ashburn-1,us-phoenix-1 --running

# Compare resources across regions
cloudtop --oracle --all-regions --region-display separate
```

### Cost Tracking

```bash
# Show total costs across all regions
cloudtop --all --all-regions --region-display grouped

# Compare costs between regions
cloudtop --oracle --all-regions --region-display separate
```

### Regional Capacity Planning

```bash
# See resource distribution
cloudtop --all --all-regions --wide

# Focus on specific resource types across regions
cloudtop --oracle --service compute --all-regions
```

## Provider Implementation Notes

### Oracle Cloud Infrastructure

The Oracle provider now:
- Queries all configured regions when `query_all_regions: true`
- Properly sets region information for each resource
- Handles region-specific API endpoints
- Continues on error to query remaining regions

### Extending to Other Providers

To add multi-region support to other providers:

1. Store regions and query_all_regions from Options during Initialize()
2. Implement getRegionsToQuery() logic to determine which regions to query
3. Create a per-region query function
4. Aggregate results from all regions
5. Set the region field correctly for each resource

## Configuration Examples

### Multi-Region Oracle Setup

```json
{
  "providers": {
    "oracle": {
      "enabled": true,
      "query_all_regions": true,
      "auth": {
        "method": "service_account",
        "key_file": "~/.oci/config"
      },
      "regions": [
        "us-ashburn-1",
        "us-phoenix-1",
        "us-sanjose-1",
        "ca-toronto-1",
        "eu-frankfurt-1"
      ],
      "services": ["compute", "containers", "autonomous_db"],
      "options": {
        "compartment_id": "ocid1.compartment.oc1..xxx"
      }
    }
  }
}
```

### Azure Multi-Region (Example)

```json
{
  "providers": {
    "azure": {
      "enabled": true,
      "query_all_regions": true,
      "regions": [
        "eastus",
        "westus2",
        "centralus",
        "westeurope"
      ],
      "services": ["vms", "aks", "functions"],
      "options": {
        "subscription_id": "your-subscription-id"
      }
    }
  }
}
```

## Performance Considerations

- **Parallel Queries**: Regions are queried concurrently to minimize latency
- **Caching**: Results are cached per-region to reduce API calls
- **Rate Limiting**: Per-provider rate limits apply across all regions
- **Timeout Handling**: Individual region failures don't block the entire query

## Migration from Single-Region

If you're upgrading from single-region configuration:

1. Add the `regions` array with your current region
2. Set `query_all_regions: false` to maintain current behavior
3. Test with `--all-regions` flag before enabling in config
4. Gradually add additional regions as needed

```json
// Before
{
  "oracle": {
    "enabled": true,
    "options": {
      "region": "us-ashburn-1"
    }
  }
}

// After
{
  "oracle": {
    "enabled": true,
    "query_all_regions": false,
    "regions": ["us-ashburn-1"],
    "options": {}
  }
}
```

## Troubleshooting

### Region Query Failures

If a region fails to respond:
- CloudTop logs a warning and continues with other regions
- Check provider credentials have access to all configured regions
- Verify region names match provider documentation

### Performance Issues

If queries are slow:
- Reduce the number of regions queried
- Enable caching with appropriate TTL
- Use `--regions` flag to filter specific regions
- Adjust rate limits if hitting API throttles

### Display Issues

If aggregated stats are incorrect:
- Ensure resources have region field set correctly
- Check that provider implements multi-region correctly
- Verify filter.Regions is being respected
