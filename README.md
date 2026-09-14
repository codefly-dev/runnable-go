# Codefly Runnable for Go

The Codefly language agent for typed, finite Go operations: generate a handler and harness, compile portable native artifacts, and emit container build recipes.

**Status: the agent process, the Builder load path, typed generation and the invocation harness are implemented. No release is published yet.** The `runnable-go` executable serves the agent lifecycle over gRPC and advertises `BUILDER`, so a runnable can be loaded, scaffolded and generated through the real agent. It advertises no Runtime: a Runnable's invocation process is supervised by its caller. [Issue #2](https://github.com/codefly-dev/runnable-go/issues/2) tracks the rest of the execution lifecycle: native packaging, build evidence and native/k3d qualification.

`pkg/generate` turns a runnable's declared contract into the typed Go bindings and the author handler scaffold. The bounded profile maps to `string`, `int64`, `bool`, generated structs and `List`, and the contract's independent `optional` (the key may be absent) and `nullable` (the value may be null) declarations map to four distinct Go spellings, one per combination, so neither state can stand for the other. Generated files carry a `//go:build go1.24` constraint: absence is kept by the `encoding/json` `omitzero` option, which an older toolchain ignores in silence rather than rejecting.

`main.go` is the agent process, at the repository root because the manifest it embeds is: `go:embed` cannot reach outside its own directory, and a second copy of the agent's identity could drift from the published one. It serves the agent lifecycle over gRPC through `agents.Serve` — identity, capabilities and plugin commands, behind core's auth, health and handshake — and registers the Builder it advertises.

`pkg/agent` serves that Builder. `Load` resolves the `RunnableLocation` the CLI sends and re-derives both the release identity and the directory from the workspace declaration, so a request naming one release while pointing at another's directory is refused rather than generated into. `Create` scaffolds the author's handler and `go.mod` without overwriting either, then writes the generated bindings and harness beside them, bound to the release `Load` resolved. `RunnableBuildInputs` and `Package` report `UNSUPPORTED`, the contract's own word for a phase an agent does not implement: this agent generates the harness but neither compiles nor archives it, so an artifact digest and a launch command could only be guessed.

`harness.go` and `codefly/main.go` are the generated invocation harness: the `codefly.runnable/v1` framing, the payload validation the bindings do not do, and the process lifecycle a launcher observes. Unlike the Python agent, which ships a harness package into the artifact, a Go harness is generated as standard-library-only source and compiled into the runnable — a contract carried beside a compiled binary could be edited after the build that measured it, while one compiled in is covered by the digest of the artifact that carries it. The harness shares the author's package so the caller's invocation identity reaches `Handle` through `InvocationFrom(ctx)`; the executable is a separate `main` because an imported package cannot also be one.

Bindings do not replace payload validation. The generated types carry the value types, the null states and the array states and nothing more, so the harness enforces required fields, undeclared keys, the signed 64-bit integer range and the refusal to coerce against the raw payload before decoding input or accepting output.

Exit codes are diagnostics rather than outcomes, and they are the ones the Python harness reports for the same conditions: `64` invalid input, `65` invalid or oversized output, `66` failure, `67` deadline, `68` interruption, `69` invalid framing. Only a validated success and an explicit `Fail(code, message)` are certain; every other end writes no result at all, so an unproven effect is never reported as one a caller may act on. The harness clears any document at the result path before reading the request, so an earlier attempt's result can never be read as this invocation's.

## What belongs here

Go handler templates, generated input/output types, the invocation harness, module/toolchain preparation, native compilation and packaging, and image recipes. Generated user handlers belong in their owner workspace.

| Component | Responsibility |
|---|---|
| [Codefly core](https://github.com/codefly-dev/core) | Generic Runnable resource, shared protocol/types and package/binding verification |
| This repository | Go generation, harness, toolchain/dependencies and build recipes |
| [Codefly CLI](https://github.com/codefly-dev/cli/issues/638) | Agent invocation over gRPC, commands, local process supervision and application image build/publish |
| [Orchestration](https://github.com/obin-ai/module-runtime/issues/63) | Installed releases, tasks, recorded invocations, progress and recovery |

The Go agent uses the same language-neutral handoff as Python. Adding it must not require another language-name branch in CLI or Orchestration. Agents emit image recipes; CLI executes and publishes application image builds.

## Shared baseline and delivery

This repository builds against `codefly-dev/core v0.3.31`. [Core PR #471](https://github.com/codefly-dev/core/pull/471) introduced `runnable.codefly.yaml`, agent kind `codefly:runnable` (`Agent_RUNNABLE`) and the immutable `RunnablePackage` and `RunnableBinding` contracts; [#473](https://github.com/codefly-dev/core/pull/473) and [#474](https://github.com/codefly-dev/core/pull/474) then closed [core #472](https://github.com/codefly-dev/core/issues/472), carrying a `RunnableLocation` through `Builder.Load`, adding `Builder.RunnableBuildInputs`, returning the native launch command from `Builder.Package`, and freezing the `codefly.runnable/v1` invocation framing. The agent name is `go`; distribution uses the `runnable-go` prefix.

The agent/CLI interface and the invocation framing are now merged in core, and [runnable-python](https://github.com/codefly-dev/runnable-python) has qualified them with a working harness and native packaging. This repository implements that same proven contract — reusing its framing rather than defining a Go-specific one — and repeats the native and actual Kubernetes invocation path with a separate generated Go example.

Qualification includes real typed I/O, cross-language schema compatibility, failure/interruption, immutable version selection, reconnect behavior and scoped cleanup. Direct binary execution or a manually submitted Kubernetes Job alone does not establish the durable execution path.

[Core #470](https://github.com/codefly-dev/core/issues/470) remains the parent delivery specification. Repository creation does not bring domain-module adoption, engine changes, infra-base or canonical handbook work into this milestone.

See [LICENSE](LICENSE) for the repository's licensing terms, matching the existing Codefly service agents.
