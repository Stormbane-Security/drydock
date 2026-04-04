# Drydock

Drydock is a general-purpose sandbox infrastructure builder and test runner.

It solves the problem of: "I need to spin up a disposable environment, run something against it, collect the results, and tear it all down cleanly — every time, reliably."

## What it does

- Define test scenarios declaratively in a single YAML file
- Spin up isolated Docker Compose environments with inline service definitions
- Run scanner assertions (beacon, classify) or custom commands against the environment
- Poll ready checks until services are healthy
- Collect artifacts: stdout/stderr, container logs, exit codes, timing
- Tear everything down cleanly, even on failure
- Run scenarios in parallel with automatic port remapping

## Architecture

```
CLI
 └─→ Scenario (YAML loader + validator)
      └─→ Engine (lifecycle orchestration)
           ├─→ ComposeGen (generates compose.yaml from inline services)
           ├─→ Backend (Docker Compose: up/wait/logs/down)
           ├─→ Assertion (beacon scan, classify, HTTP, file, port checks)
           └─→ Runner (shell command execution)
                └─→ Artifacts (output collection)
```

### Lifecycle

```
validate → generate compose → create → ready check → assertions → collect → destroy
```

### Key packages

| Package | Responsibility |
|---------|---------------|
| `cmd/drydock` | CLI entry point (run, validate, debug, bootstrap) |
| `internal/scenario` | YAML loading, validation, types, compose generation |
| `internal/engine` | Lifecycle orchestration, ready checks, debug mode |
| `internal/assertion` | Beacon/classify/HTTP/file/port assertion runners |
| `internal/backend/compose` | Docker Compose backend |
| `internal/backend/ghactions` | GitHub Actions workflow testing backend |
| `internal/runner` | Command execution (RunExec for safe argv) |
| `internal/artifact` | Output/log collection |
| `internal/interpolate` | `${fixture.*}` variable interpolation |

## Usage

```bash
# Run all test manifests in a directory (recursive)
drydock run ~/beacon/tests

# Run a single test manifest
drydock run ~/beacon/tests/databases/redis.yaml

# Run tests in parallel (4 at a time)
drydock run --parallel 4 ~/beacon/tests

# Validate manifests without running
drydock validate ~/beacon/tests

# Start services for manual debugging (no assertions)
drydock debug ~/beacon/tests/databases/redis.yaml

# Set up cloud test accounts (GCP/AWS WIF, registries)
drydock bootstrap
```

## Test manifest format

Test manifests are single YAML files with inline Docker Compose services. No separate `compose.yaml` needed — drydock generates it from the `services:` block.

```yaml
name: redis-no-auth
description: "Redis 7 with no authentication — beacon detects unauthenticated access"
tags: [redis, database, portscan]

services:
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    command: ["redis-server", "--protected-mode", "no"]

ready:
  cmd: "nc -z localhost 6379"
  timeout: 30s
  interval: 2s

assertions:
  - name: detects-unauthenticated-redis
    type: beacon
    target: redis:6379
    args: ["--no-enrich", "--scanners", "portscan", "--ports", "6379"]
    expect:
      check_id: port.redis_unauthenticated

timeout: 5m
```

### Manifest fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique test name |
| `description` | no | What is being tested |
| `tags` | no | Filterable tags |
| `services` | yes | Docker Compose service definitions (inline) |
| `ready` | no | Health check polled before assertions run |
| `ready.cmd` | yes | Shell command that must exit 0 |
| `ready.container` | no | Run ready check inside a compose service |
| `ready.timeout` | no | Max wait time (default 60s) |
| `ready.interval` | no | Poll interval (default 3s) |
| `commands` | no | Shell commands to run after services start (e.g. DB seeding) |
| `assertions` | yes | List of assertions to verify |
| `timeout` | no | Overall test timeout (default 5m) |

### Service definition

Services support all standard Docker Compose fields:

```yaml
services:
  my-app:
    image: nginx:alpine           # or build: { context: ./app, dockerfile: Dockerfile }
    ports: ["8080:80"]
    environment:
      DB_HOST: postgres
    volumes:
      - ./config/nginx.conf:/etc/nginx/nginx.conf:ro
    depends_on: [postgres]
    command: ["nginx", "-g", "daemon off;"]
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost"]
      interval: 10s
      timeout: 5s
    cap_add: [NET_ADMIN]
    networks: [backend]
    restart: "no"
```

### Assertion types

**beacon** — Run a beacon scan and check for expected findings:
```yaml
- name: detects-cors-misconfig
  type: beacon
  target: app:8080
  args: ["--no-enrich", "--scanners", "cors", "--deep", "--permission-confirmed", "--ports", "8080"]
  expect:
    check_id: web.cors_misconfiguration        # required: finding must have this check_id
    evidence_key: reflected_origin              # optional: evidence field name
    evidence_value: "https://evil.example.com"  # optional: evidence field value (substring match)
    min_findings: 1                             # optional: minimum total findings
```

**classify** — Run beacon classify and check evidence fields:
```yaml
- name: identifies-nginx
  type: classify
  target: app:80
  expect:
    server: nginx
```

**Scan modes:** Add `--deep --permission-confirmed` for deep-mode scanners, or `--authorized` for exploitation-class scanners (drydock auto-sets `BEACON_AUTHORIZED_ACK=1`).

### Docker network isolation

Scanner assertions run inside a Docker container on the scenario's compose network. This ensures scans only see test infrastructure, not host services. Set `DRYDOCK_SCANNER_IMAGE` to override the default `beacon-scanner` image, or `DRYDOCK_SCANNER_BIN_LINUX` to mount a local binary.

## Test organization (beacon)

Beacon's test manifests live in `~/beacon/tests/` organized by category:

```
tests/
  databases/       # Redis, MySQL, PostgreSQL, MongoDB, etc.
  services/        # Kafka, RabbitMQ, Jenkins, Grafana, etc.
  scanners/        # CORS, SQLi, XSS, SSRF scanner tests
  exposure/        # Exposed files, debug endpoints, misconfigs
  fingerprint/     # Technology detection and classification
  nonstandard-port/ # Services on unexpected ports
  email/           # SPF/DMARC/DKIM email security
  dlp/             # Data loss prevention pattern matching
  infrastructure/  # Web servers, proxies, service mesh
  cve/             # CVE-specific vulnerable versions
  cicd/            # GitLab CE, TeamCity self-hosted
  identity/        # SAML, LDAP injection, SCIM
  web-vulns/       # Path traversal, deserialization, smuggling
  dns/             # DNS open resolver, zone transfer
  e2e/             # Full pipeline integration tests
```

## Development

```bash
make build    # build the binary
make test     # run tests
make vet      # run go vet
make check    # vet + test
make clean    # remove binary and runtime artifacts
```

Requires: Go 1.25+, Docker with Compose v2.
