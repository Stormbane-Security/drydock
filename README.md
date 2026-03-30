# Drydock

Drydock is a general-purpose sandbox infrastructure builder and test runner.

It solves the problem of: "I need to spin up a disposable environment, run something against it, collect the results, and tear it all down cleanly — every time, reliably."

## What it does

- Define a test scenario declaratively (YAML)
- Build an isolated local sandbox (Docker Compose)
- Run commands or tests against the environment
- Collect artifacts: stdout/stderr, container logs, exit codes, timing
- Tear everything down cleanly, even on failure

## v0 scope

v0 is intentionally narrow:

- Local execution only
- Docker Compose as the only environment backend
- Declarative YAML scenario definitions
- CLI interface: `validate`, `run`, `destroy`, `inspect`
- Shell command execution against created environments
- Artifact collection to local disk
- Reliable teardown including cleanup on failure

## Out of scope (for now)

- Kubernetes, Terraform, or any cloud backend
- Remote execution or agents
- Web UI, auth, queues, database
- AI orchestration
- Plugin system

## Architecture

```
CLI
 └─→ Scenario (YAML loader + validator)
      └─→ Engine (lifecycle orchestration)
           ├─→ Backend (Docker Compose: up/wait/logs/down)
           └─→ Runner (shell command execution)
                └─→ Artifacts (output collection)
```

### Lifecycle

```
validate → prepare → create → wait → execute → collect → destroy
```

### Key packages

| Package | Responsibility |
|---------|---------------|
| `cmd/drydock` | CLI entry point |
| `internal/scenario` | YAML loading, validation, types |
| `internal/engine` | Lifecycle orchestration |
| `internal/backend/compose` | Docker Compose backend |
| `internal/runner` | Command execution |
| `internal/artifact` | Output/log collection |

## Usage

```bash
# Validate a scenario definition
drydock validate ./scenarios/example

# Run a scenario end-to-end
drydock run ./scenarios/example

# Destroy a running scenario environment
drydock destroy ./scenarios/example

# Inspect results from a previous run
drydock inspect <run-id>
```

## Scenario definition

Scenarios are defined in YAML. See `scenarios/example/scenario.yaml` for a working example.

```yaml
name: my-test
backend:
  type: compose
  compose_file: compose.yaml
commands:
  - name: check-service
    run: curl -sf http://localhost:8080/health
timeout: 5m
artifacts:
  - container_logs
```

## Development

```bash
make build    # build the binary
make test     # run tests
make vet      # run go vet
make check    # vet + test
make clean    # remove binary and runtime artifacts
```

Requires: Go 1.22+, Docker with Compose v2.
