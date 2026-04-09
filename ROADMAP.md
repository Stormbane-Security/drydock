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
