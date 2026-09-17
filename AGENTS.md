# Working in codefly-dev/runnable-go

`github.com/codefly-dev/runnable-go` (Go 1.27) is the Codefly Runnable agent for
Go: one executable a caller starts over gRPC, which turns a runnable's declared
contract into typed Go bindings, an author handler scaffold, and the
`codefly.runnable/v1` invocation harness.

It owns Go generation, the generated harness, module and toolchain preparation,
and — once implemented — native compilation, packaging and image recipes.

It does **not** own: the Runnable resource model, the agent gRPC contracts or
the invocation framing (`codefly-dev/core`, consumed at a pinned version); agent
invocation, process supervision and image publishing (`codefly-dev/cli`);
installed releases and recorded invocations (`obin-ai/module-runtime`); or the
handlers authors write in their own workspaces. This agent advertises `BUILDER`
and no Runtime — a Runnable's invocation process is supervised by its caller,
never by this repository.

The handoff is language-neutral, and `runnable-python` qualified it first. A
change here that would need a language-name branch in the CLI or in
Orchestration is the wrong change.

## How to behave

Fleet standard — [handbook#68](https://github.com/obin-ai/handbook/issues/68).
They land hard here: almost nothing this repo produces runs in this repo. It
emits source someone else compiles and a harness someone else supervises, so a
defect is observed one or two systems away from what caused it.

- **A gap in the tooling is a bug in the tooling — never a reason to reach
  around it.** When a step `codefly`, core or a caller does not perform is in
  the way, the answer is a capability fixed in whichever repo owns it, named in
  the PR. Never a hand-assembled substitute — not as a "workaround", not "just
  this once", not "until the capability lands".
- **Never hack. Provide the best fix, even when it spans repos.** The fix living
  in `codefly-dev/core` or `codefly-dev/cli` is not a reason to work around it
  here; open the PR there and consume the reviewed result. When it genuinely
  cannot be fixed now, the deliverable is a precise issue against that owner
  plus an explicitly labelled stopgap — never an unlabelled one.
- **Classify every change that makes something work**, in the PR body: a *fix*
  at the place that owns the behaviour, or a *hack*. A hack does not become a
  fix by working, by being small, by being local, or by the real fix belonging
  elsewhere.
- **Never hardcode what the system resolves** — injected environment, derived
  ports, service addresses, credentials copied out of another component. Here
  that extends to what a request resolves: the release identity and the
  directory are re-derived from the workspace declaration, never taken from the
  request's spelling. Typing one encodes something true only on one machine for
  ten minutes, and it fails quietly — a runtime missing a credential can skip
  registration *silently*, so the service boots, serves, and is simply absent.
- **Diagnose, do not pattern-match.** "It started working when I set X" is not a
  diagnosis — set X back and confirm it breaks. Do not trust an error message
  before checking its claim: a compile failure in a generated package has meant
  the test's own fixture, not the generator under test.
- **Say what you did not verify.** Unverified is not the same as working.
  Generation passing is not the generated code compiling, and compiling is not
  the harness surviving a real invocation. If you could not exercise it, the PR
  says so.

## Build and test

Derived from `.github/workflows/ci.yml`, which pins Go from `go.mod` and runs
exactly these three, all of them locally runnable:

```bash
go build ./...
go test ./...
go vet ./...
```

```bash
go test ./pkg/generate/ -run TestGenerated -v   # one package, one group
UPDATE_GOLDEN=1 go test ./pkg/generate/         # rewrite testdata/*.golden
```

`UPDATE_GOLDEN=1` writes the golden rather than comparing it. Read the resulting
diff before committing it — the golden exists because generated output is read
as a diff by the author whose workspace it lands in, and accepting it unread
defeats the test entirely.

**Never mock.** The suite builds the agent binary and speaks gRPC to it
(`serve_test.go`), compiles and runs generated packages with the real toolchain
(`pkg/generate/compile_test.go`), and executes a built harness as a process
(`pkg/generate/invocation_test.go`). Those runs set `GOPROXY=off`, so generated
code must stay standard-library-only or the suite stops building. If a boundary
is hard to reach, reach it anyway.

## Where things live

| Path | Owns |
| --- | --- |
| `main.go`, `agent.codefly.yaml` | the agent process and the identity it embeds |
| `pkg/agent/agent.go` | identity, capabilities, plugin commands |
| `pkg/agent/builder.go` | `Load`, `Create`, `RunnableBuildInputs`, `Package` |
| `pkg/generate/bindings.go` | typed input/output from the bounded profile |
| `pkg/generate/handler.go` | the author's scaffold, written once |
| `pkg/generate/harness.go` | `codefly.runnable/v1` framing, validation, lifecycle |
| `pkg/generate/paths.go` | `Resolve` / `Within` / `Confine` |
| `pkg/generate/project.go` | package naming, entrypoint and build inputs |

The README carries the design rationale — why the harness is compiled in rather
than shipped beside the artifact, why absence and null are four distinct Go
spellings, what each exit code means. Read it rather than re-deriving it.

## Rules that bite

- **The author's files are the author's.** `Create` scaffolds `handler.go` and
  `go.mod` only when absent and never overwrites either; generated files are
  written beside them. Regeneration must stay safe to run twice.
- **Resolve a path before trusting it.** Anything derived from a request goes
  through `generate.Confine`; a prefix test on an unresolved path proves
  nothing, and a dangling symlink is refused rather than treated as missing.
- **`UNSUPPORTED` is an answer.** `RunnableBuildInputs` and `Package` report it
  because this agent neither compiles nor archives: an artifact digest or a
  launch command could only be guessed. Never return a guess where the contract
  has a word for not knowing.
- **An unproven effect is never reported as one.** The harness writes a result
  only on validated success or an explicit `Fail`; every other end writes
  nothing, and it clears any document at the result path before reading the
  request so a previous attempt cannot be read as this invocation's.
- **The generated language version lives in one place.** `generate.GoRequirement`
  is the `//go:build` constraint on generated files; the compile test reads the
  `go` directive from this repository's `go.mod` rather than repeating it. Keep
  it that way — a second copy drifts silently.
- **Bindings are not validation.** Generated types carry value, null and array
  states and nothing more; required fields, undeclared keys, the signed 64-bit
  range and the refusal to coerce are enforced by the harness against the raw
  payload.

## Workflow

- Branch and PR; never commit to `main`. Conventional Commits for the title.
- Keep this file under ~150 lines (hard cap 200). Push depth into a nested
  `AGENTS.md` beside what it describes, into `.claude/skills/`, or into the
  README.
- If a `CLAUDE.md` is ever added, it is a one-line `@AGENTS.md` pointer. One
  canonical source.
- Treat this file as code: the PR that changes a process updates it.
