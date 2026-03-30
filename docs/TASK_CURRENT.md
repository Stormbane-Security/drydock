# Current Tasks

## Milestone: v0 Happy Path

Get to a working end-to-end flow:
`drydock run ./scenarios/example` validates → brings up compose → runs a command → collects output → tears down.

### In Progress

- [x] Initialize repo, Go module, project skeleton
- [x] Create README, DECISIONS.md, TASK_CURRENT.md
- [ ] Implement scenario types and YAML loader
- [ ] Add scenario validation
- [ ] Add scenario tests
- [ ] Implement Compose backend (up/wait/down/logs)
- [ ] Implement command runner
- [ ] Implement artifact collection
- [ ] Implement engine lifecycle orchestration
- [ ] Create example scenario
- [ ] Implement CLI (validate, run, destroy, inspect)
- [ ] End-to-end happy path working

### Blocked / Not Started

- Nothing blocked. Docker Compose availability on dev machine is the only external dependency.

### Decisions Pending

- Artifact directory layout (proposed: `.drydock/runs/<run-id>/`)
- Run ID format (proposed: `<scenario-name>-<timestamp>`)
- Readiness strategy (proposed: Compose healthchecks + timeout)
