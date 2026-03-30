# Architecture Decision Records

## ADR-001: Docker Compose only for v0

**Status:** Accepted

**Context:** Drydock needs a backend to create isolated environments. Options include Docker Compose, Kubernetes, Terraform, Podman, and others.

**Decision:** v0 supports only Docker Compose.

**Rationale:**
- Compose is available on every developer machine with Docker installed
- No cloud credentials or cluster access required
- Fast startup (~seconds vs minutes for k8s)
- Declarative YAML that most developers already understand
- Simple lifecycle: `docker compose up` / `docker compose down`
- Sufficient for integration tests, service mocks, and multi-container scenarios

**Consequences:** Users who need k8s-native testing must wait for a future backend. The Backend interface should be clean enough to add alternatives later without restructuring.

---

## ADR-002: Local execution only for v0

**Status:** Accepted

**Context:** Drydock could support remote execution, distributed agents, or cloud-hosted sandboxes.

**Decision:** v0 runs entirely on the local machine.

**Rationale:**
- Eliminates networking, auth, and coordination complexity
- No infrastructure required beyond Docker
- Faster feedback loop for developers
- Easier to debug when things go wrong
- Remote execution can be layered on later without changing the core lifecycle

**Consequences:** Cannot run scenarios on CI machines from a local CLI. Users must have Docker locally. This is acceptable for v0.

---

## ADR-003: Declarative YAML scenario definitions

**Status:** Accepted

**Context:** Scenarios could be defined imperatively (Go code), declaratively (YAML/JSON), or via CLI flags.

**Decision:** Use declarative YAML files stored in the repository.

**Rationale:**
- Version-controllable alongside the code they test
- Readable by non-Go developers
- Separates "what to test" from "how to run it"
- YAML is the standard format for infrastructure config (Compose, k8s, CI)
- JSON would work but is noisier for humans; YAML supports comments

**Consequences:** Scenarios cannot contain conditional logic. This is intentional — complex orchestration belongs in the commands being run, not the scenario definition.

---

## ADR-004: General platform, not Beacon-specific

**Status:** Accepted

**Context:** This project originated from integration testing needs in Beacon (a security scanner). It could be built as a Beacon-internal test harness or as a standalone tool.

**Decision:** Build Drydock as a general-purpose platform with no Beacon-specific code.

**Rationale:**
- A general tool is useful across multiple projects
- Forces cleaner abstractions (no shortcuts that assume Beacon internals)
- Beacon can consume Drydock as a dependency or CLI tool later
- Other projects (web services, databases, microservices) have the same "spin up, test, tear down" need

**Consequences:** No Beacon-specific shortcuts. Scenario definitions must be self-contained. This may require slightly more configuration in Beacon's scenarios, but the tradeoff is worth it.

---

## ADR-005: Shell out to `docker compose` instead of using Go SDK

**Status:** Accepted

**Context:** The Compose backend could use the Docker Go SDK or shell out to the `docker compose` CLI.

**Decision:** Shell out to `docker compose`.

**Rationale:**
- The Docker Go SDK does not natively support Compose — it operates on individual containers
- The `docker/compose` Go library exists but is heavy, under-documented, and tightly coupled to the CLI internals
- Shelling out is simpler, more debuggable, and matches what users do manually
- Output from `docker compose` commands can be captured directly as artifacts
- Fewer dependencies in go.mod

**Consequences:** Requires `docker compose` (v2) to be installed on the host. This is a reasonable requirement — Docker Desktop and modern Docker Engine include it by default.
