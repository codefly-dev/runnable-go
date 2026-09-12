# Codefly Runnable for Go

The Codefly language agent for typed, finite Go operations: generate a handler and harness, compile portable native artifacts, and emit container build recipes.

**Status: the contract-determined generation slice is implemented; the agent process, harness and qualification are deferred until the Python/native/k3d contract is qualified.** No agent executable or release is available yet. [Implementation issue #1](https://github.com/codefly-dev/runnable-go/issues/1) tracks this work.

`pkg/generate` turns a runnable's declared contract into the typed Go bindings and the author handler scaffold. The bounded profile maps to `string`, `int64`, `bool`, generated structs and slices, and the contract's independent `optional` (the key may be absent) and `nullable` (the value may be null) declarations map to three distinct Go spellings so neither state can stand for the other.

The agent gRPC process, the invocation harness, build evidence and native/k3d qualification wait on [core #472](https://github.com/codefly-dev/core/issues/472), which freezes the agent/CLI handoff `runnable.codefly.yaml` names but does not define: carrying a `RunnableIdentity` through `Builder.Load`, returning native launch and build evidence from `Builder.Package`, and the `codefly.runnable/v1` framing a harness must agree with byte for byte.

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

[Core PR #471](https://github.com/codefly-dev/core/pull/471) merged at `6a40c4bf28ac3dcebd534c32040349be96626605`. It introduces `runnable.codefly.yaml`, agent kind `codefly:runnable` (`Agent_RUNNABLE`), and the immutable `RunnablePackage` and `RunnableBinding` contracts. The agent name is `go`; distribution uses the `runnable-go` prefix.

First qualify the remaining agent/CLI interface and invocation framing in [runnable-python #1](https://github.com/codefly-dev/runnable-python/issues/1) and [CLI #638](https://github.com/codefly-dev/cli/issues/638). This repository then implements that proven contract and repeats the native and actual Kubernetes invocation path with a separate generated Go example.

Qualification includes real typed I/O, cross-language schema compatibility, failure/interruption, immutable version selection, reconnect behavior and scoped cleanup. Direct binary execution or a manually submitted Kubernetes Job alone does not establish the durable execution path.

[Core #470](https://github.com/codefly-dev/core/issues/470) remains the parent delivery specification. Repository creation does not bring domain-module adoption, engine changes, infra-base or canonical handbook work into this milestone.

See [LICENSE](LICENSE) for the repository's licensing terms, matching the existing Codefly service agents.
