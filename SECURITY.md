# Security policy

`statsagent` runs as root on the hosts it measures and holds a bearer token
for the Kubernetes API and an API key for its push endpoint. `cloudtop` holds
cloud provider credentials. A way to leak those credentials, or to make either
tool trust a server it should not, is the most serious kind of bug they can
have. Please report it privately first.

## How to report

Use GitHub's **private vulnerability reporting** on this repository: the
**Security** tab, then **Report a vulnerability**. The report is visible only
to the maintainers until an advisory is published.

Please do not open a public issue or pull request for a vulnerability, and do
not post a working exploit anywhere public until a fix is released.

A useful report includes the commit you tested, the exact command line,
environment and config (with secrets removed), what the tool should have done
and what it did instead, and ideally a failing Go test that shows it.

## What counts as a vulnerability

- **Skipped certificate verification**: any path where `statsagent` or
  `cloudtop` sends a credential over TLS without verifying the server against
  the configured CA or the system roots.
- **Credentials reaching output**: an API key, bearer token or provider secret
  that appears in logs, `/metrics`, `/stats`, JSON output or error messages.
- **Code execution or file writes from input**: a crafted kubeconfig, Docker
  API response, Kubernetes API response, cloud API response or config file
  that makes either tool run commands or write outside its documented paths.

## What is not a vulnerability

- `statsagent serve` listening on all interfaces without authentication. This
  is documented: it exists so a Prometheus server can scrape it, and the port
  must be firewalled to the scraper. The data it exposes (hostnames, process
  and container names, mounts, k3s namespaces) is part of that documented
  behaviour.
- `statsagent push` to a plain `http://` endpoint. It warns; use `https://`.
- Anything that requires already being root on the host or being able to edit
  the binary, its systemd unit or its config.

## Supported versions

Only the latest commit on the default branch is supported. Fixes are made
there.
