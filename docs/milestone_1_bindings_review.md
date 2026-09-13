# Milestone 1 — reviewed Go bindings

PR #3 establishes the `codefly:runnable` / `go` distribution identity and renders
core's bounded Runnable schema as typed Go input/output bindings and an author
handler scaffold. Core is pinned at `6a40c4bf28ac`.

The review found no blocking defect in this limited implementation. Go tests
compile the generated package with the real compiler and `encoding/json`,
checking absent/null/set states, signed 64-bit integers, type mismatches and
identifier collisions. The repository's tests, build and vet must pass before
merge.

This milestone does not include a runnable agent executable, invocation harness,
native package, image recipe, release or durable invocation qualification.
[Issue #2](https://github.com/codefly-dev/runnable-go/issues/2) carries the
remaining acceptance checklist from issue #1.

Python now has a proposed harness and packaging implementation. Core #472 must
ratify the shared loading, build-evidence and invocation contracts; Go will reuse
that contract after Python qualification. No Go-specific launcher protocol or
language branch belongs in CLI or Orchestration.

The future Go harness must validate required fields, unknown keys and nullable
arrays against the raw payload. Generated Go types alone do not enforce those
rules. Native and Orchestration-created k3d invocation Jobs, cross-language
agreement, immutable version selection and reconnect remain to qualify.
