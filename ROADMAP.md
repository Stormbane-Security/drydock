# Drydock Roadmap

## Current State (2026-04-09)

Drydock is a general-purpose sandbox infrastructure builder and test runner. Used by beacon for E2E testing of security scanners against real vulnerable containers.

- **510+ test scenarios** in beacon's test suite
- **Supports**: Docker Compose backends, ready checks, command/beacon assertions, port remapping
- **CI integration**: GitHub Actions with Docker image caching (zstd compressed)

## Immediate

### Build Context Support
Some beacon tests need custom Docker images (TLS with expired certs, specific app configs). Currently these use `entrypoint` workarounds because drydock doesn't support `build:` in service definitions.

**Add**: `build:` directive support in scenario YAML → `docker-compose build` before `up`.

### Debug Mode Improvements
`drydock debug` keeps infrastructure up for manual testing. Improvements needed:
- Show beacon scan command with correct remapped ports
- Output timing for each assertion
- Show container logs on failure

### Parallel Scenario Execution
`drydock run --parallel N` runs N scenarios concurrently. Currently defaults to 1 (sequential). Verify this works reliably with port conflicts avoided via `--fixed-ports`.

## Medium Term

### Test Result Database
Store test results in SQLite for trending:
- Which tests fail most often?
- What's the average container startup time per image?
- Track flaky tests

### Remote Docker Support
Run drydock scenarios against remote Docker hosts (SSH tunnel or Docker context). Enables testing against VMs with Windows Server, real K8s clusters, etc.

### Scenario Dependencies
Allow scenarios to depend on other scenarios: "run `setup-database` before `test-sqli-chain`". Enables shared infrastructure across related tests.

---

## v2: Runtime observation platform

Drydock evolves from "compose + assertions" to a full runtime observation and testing platform.

### New assertion types

```yaml
# Load testing
- name: handles-load
  type: load
  target: http://localhost:8080/api/users
  rps: 100
  duration: 30s
  expect:
    p99_latency: "<500ms"
    error_rate: "<1%"

# Fault injection
- name: survives-db-failure
  type: fault
  kill: postgres
  duration: 10s
  expect:
    service_recovers: true
    no_crash: true

# Behavior observation
- name: no-unexpected-egress
  type: observe
  expect:
    no_outbound_except: ["postgres:5432", "redis:6379"]
    no_unexpected_processes: true
    no_file_writes_outside: ["/tmp", "/var/log"]
```

### Observation layer (no eBPF required initially)

- Process monitoring (container events, /proc)
- Egress proxy (all outbound connections logged)
- DNS capture (CoreDNS logging all queries)
- Filesystem diff (snapshot before/after, show changes)
- stdout/stderr capture (error detection)
- Port scan the app (what's actually listening)
- Secret canary traps (inject canary creds, detect exfiltration)

### What it catches beyond security

- Crashes, panics, OOM kills, segfaults
- Memory/goroutine/thread leaks
- Deadlocks (process alive but unresponsive)
- Retry storms, connection exhaustion
- Dependency failure handling (does app crash when DB is down?)
- Configuration errors (wrong port, missing env vars, debug mode)
- Schema/API response validation
- Performance degradation under load

### Product positioning

Not "we scan your code." Instead: "we run your app, exercise it, break things on purpose, and tell you everything that went wrong — before your users find out."

The gap: code review (human or AI) tells you if code is well-written. Dynamic analysis tells you if it works. Vibe-coded apps can pass code review and completely fall apart at runtime.

### Competitive landscape

Nobody combines deploy + functional tests + fault injection + behavioral observation + verdict.
- Falco: runtime monitoring, not testing
- Chaos Monkey: fault injection only, no observation
- DAST tools: security only, no load/fault/behavior
- Malware sandboxes: binary analysis, not app testing

### v3: Full platform

- kind/k3d support for K8s apps
- Deployment validation
- Behavioral baselines and regression detection
- Integration with Gauntlet for CI/CD workflow testing
- Integration with Beacon for post-deploy security scanning
