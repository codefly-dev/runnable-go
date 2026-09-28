package generate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"
	"github.com/codefly-dev/core/resources"
	corerunnable "github.com/codefly-dev/core/runnable"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/codefly-dev/runnable-go/pkg/generate"
)

// The harness is qualified against core rather than against this package's own
// idea of the contract: every call carries core's own header spellings and is
// sent to the procedure core pins, and every outcome is the one core's
// ClassifyServed concludes. A harness that agreed only with its own tests would
// agree with no caller.

const (
	releaseName      = "word-count"
	releaseModule    = "proof"
	releaseWorkspace = "proof"
	releaseVersion   = "0.0.1"
	packagePkg       = "wordcount"
)

func release() *basev0.RunnableIdentity {
	return &basev0.RunnableIdentity{
		Name:      releaseName,
		Module:    releaseModule,
		Workspace: releaseWorkspace,
		Version:   releaseVersion,
	}
}

// declaration is the runnable the proof package implements: every value type of
// the bounded profile, and optional and nullable declared apart.
func declaration(t *testing.T, cancellation resources.RunnableCancellation, recovery resources.RunnableRecovery) *resources.Runnable {
	t.Helper()
	runnable := &resources.Runnable{
		Kind:    resources.RunnableKind,
		Name:    releaseName,
		Version: releaseVersion,
		Agent: &resources.Agent{
			Kind: resources.RunnableAgent, Name: "go", Publisher: "codefly.dev", Version: "0.0.1",
		},
		Contract: contractOf(
			[]*resources.RunnableField{
				field("text", resources.RunnableFieldString),
				{Name: "repeat", Type: resources.RunnableFieldInteger, Optional: true},
				{Name: "note", Type: resources.RunnableFieldString, Nullable: true},
				{Name: "options", Type: resources.RunnableFieldObject, Optional: true, Fields: []*resources.RunnableField{
					field("limit", resources.RunnableFieldInteger),
				}},
				{Name: "tags", Type: resources.RunnableFieldArray, Items: field("", resources.RunnableFieldString)},
			},
			[]*resources.RunnableField{
				field("total", resources.RunnableFieldInteger),
				field("echo", resources.RunnableFieldString),
			},
		),
		Entrypoint: &resources.RunnableEntrypoint{Handler: "handler.go"},
		Execution: &resources.RunnableExecution{
			Facilities:   []resources.RunnableFacility{resources.RunnableFacilityGenerated},
			Timeout:      "5m",
			Cancellation: cancellation,
			Recovery:     recovery,
		},
	}
	runnable.SetModule(releaseModule)
	if err := runnable.Validate(); err != nil {
		t.Fatalf("declaration is not valid: %v", err)
	}
	return runnable
}

// compiled is a generated runnable built the way a placement would receive it.
type compiled struct {
	runnable *resources.Runnable
	pkg      *basev0.RunnablePackage
	binary   string
}

// compile generates the runnable, writes the author's handler and builds the
// executable that serves it. The module resolves nothing from the network:
// generated Go depends on the standard library alone, which is what lets a
// package be built and archived without a proxy.
func compile(t *testing.T, runnable *resources.Runnable, handler string) compiled {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(handler), 0o600); err != nil {
		t.Fatalf("writing the handler: %v", err)
	}
	// Scaffold writes the module declaration and leaves the handler alone.
	if err := generate.Scaffold(runnable, dir); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if err := generate.GenerateForRelease(runnable, dir, release()); err != nil {
		t.Fatalf("GenerateForRelease: %v", err)
	}

	binary := filepath.Join(t.TempDir(), "runnable")
	build := exec.Command("go", "build", "-o", binary, "./"+generate.CommandDirectory)
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	if output, err := build.CombinedOutput(); err != nil {
		source, _ := os.ReadFile(filepath.Join(dir, generate.HarnessFile))
		t.Fatalf("building the generated runnable: %v\n%s\n--- harness ---\n%s", err, output, source)
	}
	return compiled{runnable: runnable, pkg: corePackageOf(t, runnable), binary: binary}
}

// corePackageOf assembles the immutable package descriptor core validates an
// invocation against, from the same declaration the harness was generated from.
// The build and artifact facts are stand-ins: this agent does not package yet,
// and none of them reaches the harness.
func corePackageOf(t *testing.T, runnable *resources.Runnable) *basev0.RunnablePackage {
	t.Helper()
	digest := "sha256:" + strings.Repeat("ab", 32)
	agent, err := runnable.Agent.Proto()
	if err != nil {
		t.Fatalf("agent proto: %v", err)
	}
	prepared, err := corerunnable.PreparePackage(&basev0.RunnablePackage{
		Schema:    corerunnable.PackageSchemaV1,
		Identity:  release(),
		Agent:     agent,
		Contract:  runnable.Contract.Proto(),
		Execution: runnable.Execution.Proto(),
		Build: &basev0.RunnableBuild{
			Handler:             &basev0.RunnableInputDigest{Path: "handler.go", Digest: digest},
			Inputs:              []*basev0.RunnableInputDigest{{Path: generate.ModuleFile, Digest: digest}},
			HarnessDigest:       digest,
			Toolchain:           "go" + strings.TrimPrefix(runtime.Version(), "go"),
			ConfigurationDigest: digest,
		},
		Artifacts: []*basev0.RunnableArtifact{{
			Kind:      basev0.RunnableArtifact_ARCHIVE,
			Platform:  runtime.GOOS + "/" + runtime.GOARCH,
			Reference: "word-count.tar.gz",
			Digest:    digest,
			Command:   []string{generate.CommandDirectory},
		}},
	})
	if err != nil {
		t.Fatalf("PreparePackage: %v", err)
	}
	return prepared
}

// resultDocument renders a RunnableResult as the proto3 JSON core parses.
func resultDocument(t *testing.T, result *basev0.RunnableResult) []byte {
	t.Helper()
	document, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(result)
	if err != nil {
		t.Fatalf("encoding the result: %v", err)
	}
	return document
}
