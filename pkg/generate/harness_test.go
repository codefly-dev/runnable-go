package generate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"
	"github.com/codefly-dev/core/resources"
	corerunnable "github.com/codefly-dev/core/runnable"

	"github.com/codefly-dev/runnable-go/pkg/generate"
)

func harnessSource(t *testing.T, runnable *resources.Runnable, identity *basev0.RunnableIdentity) string {
	t.Helper()
	source, err := generate.Harness(packagePkg, runnable, identity)
	if err != nil {
		t.Fatalf("Harness: %v", err)
	}
	return string(source)
}

// The framing is one contract across the agents that implement it, and it is
// core's. A Go harness that spelled an environment variable or an exit code
// differently from the Python one would need a launcher of its own, which is
// the thing the shared protocol exists to prevent.
//
// The values are read from core here rather than repeated, so a rename in core
// fails this test instead of silently splitting the two harnesses apart.
func TestTheFramingIsOneContract(t *testing.T) {
	source := harnessSource(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), release())

	for _, value := range []string{
		corerunnable.EnvProtocol,
		corerunnable.EnvInvocationPath,
		corerunnable.EnvResultPath,
		resources.RunnableProtocolV1,
	} {
		if !strings.Contains(source, fmt.Sprintf("%q", value)) {
			t.Errorf("the harness does not carry %q, so it reads a framing no core launcher writes", value)
		}
	}

	// The exit codes the Python harness reports for the same conditions. They
	// are diagnostics rather than outcomes, which is exactly why they have to
	// agree: an operator reading 64 must learn the same thing from either.
	for name, code := range map[string]int{
		"harnessExitCompleted":     0,
		"harnessExitInvalidInput":  64,
		"harnessExitInvalidOutput": 65,
		"harnessExitFailed":        66,
		"harnessExitTimeout":       67,
		"harnessExitInterrupted":   68,
		"harnessExitProtocol":      69,
	} {
		if !declares(source, name, fmt.Sprint(code)) {
			t.Errorf("the harness does not declare %s = %d", name, code)
		}
	}
}

// The harness refuses an invocation naming another release, which is what lets
// two versions of one runnable be installed at once without either answering
// the other's calls. It can only do that if the release it was built for is in
// it.
func TestTheHarnessIsBoundToTheReleaseItWasGeneratedFor(t *testing.T) {
	runnable := declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute)

	resolved := fmt.Sprintf("var harnessRelease = Release{Name: %q, Module: %q, Workspace: %q, Version: %q}",
		releaseName, releaseModule, releaseWorkspace, releaseVersion)
	if source := harnessSource(t, runnable, release()); !strings.Contains(source, resolved) {
		t.Errorf("the harness is not bound to the resolved release; want\n\t%s", resolved)
	}

	// Standalone generation has no owning workspace, so there is nothing to
	// check an invocation's workspace against and the harness says so rather
	// than inventing one.
	standalone := fmt.Sprintf("var harnessRelease = Release{Name: %q, Module: %q, Workspace: \"\", Version: %q}",
		releaseName, releaseModule, releaseVersion)
	if source := harnessSource(t, runnable, nil); !strings.Contains(source, standalone) {
		t.Errorf("standalone generation pinned a release it never resolved; want\n\t%s", standalone)
	}
}

// The bounds and policies the harness enforces are the declaration's. They are
// rendered in rather than read beside the binary, so a build that measured one
// contract cannot run another.
func TestTheHarnessCarriesTheDeclaredExecution(t *testing.T) {
	runnable := declaration(t, resources.RunnableCancellationSignal, resources.RunnableRecoveryReceipt)
	runnable.Execution.Payload = &resources.RunnablePayload{MaxInputBytes: 2048, MaxOutputBytes: 4096}
	source := harnessSource(t, runnable, release())

	// gofmt aligns a const block, so the spacing around = is not the test's to
	// predict.
	for name, value := range map[string]string{
		"harnessMaxInputBytes":  "2048",
		"harnessMaxOutputBytes": "4096",
		"harnessRecovery":       `"receipt"`,
		"harnessCancellation":   `"signal"`,
	} {
		if !declares(source, name, value) {
			t.Errorf("the harness does not declare %s = %s", name, value)
		}
	}

	// An undeclared bound is the core default, not an unbounded payload.
	bounded := harnessSource(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), release())
	if !declares(bounded, "harnessMaxInputBytes", fmt.Sprint(resources.DefaultRunnablePayloadBytes)) {
		t.Error("an undeclared payload bound did not fall back to the core default")
	}
}

// declares reports whether the generated source declares name as value. The
// declaration is matched as a whole line, because gofmt aligns a const block
// and the padding is not the test's to predict.
func declares(source string, name string, value string) bool {
	line := `(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=\s*` + regexp.QuoteMeta(value) + `\s*$`
	return regexp.MustCompile(line).MatchString(source)
}

// A contract declaring no field accepts exactly the empty object, so an agent
// generating for it still produces a harness that compiles and validates.
func TestAnEmptyContractGeneratesAnEmptySchema(t *testing.T) {
	runnable := declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute)
	runnable.Contract = contractOf(nil, nil)
	source := harnessSource(t, runnable, release())

	if !strings.Contains(source, "var harnessInputSchema = []harnessField{}") {
		t.Error("an empty input schema did not render as one")
	}
	if !strings.Contains(source, "var harnessOutputSchema = []harnessField{}") {
		t.Error("an empty output schema did not render as one")
	}
}

// Generation is what a build measures, so an unchanged declaration has to
// produce unchanged bytes: a harness that differed per run would move the
// digest of a package whose content never changed.
func TestRegeneratingAnUnchangedDeclarationProducesTheSameBytes(t *testing.T) {
	runnable := declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute)
	first := harnessSource(t, runnable, release())
	for range 3 {
		if again := harnessSource(t, runnable, release()); again != first {
			t.Fatal("regenerating an unchanged declaration produced different bytes")
		}
	}
}

// The agent owns the harness and the executable; the author owns the handler
// and the module. Generating has to write the first pair and leave the second
// alone, or regenerating a runnable would discard an implementation.
func TestGenerateWritesTheAgentsFilesAndKeepsTheAuthors(t *testing.T) {
	runnable := declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute)
	dir := t.TempDir()
	const implementation = "package wordcount\n\n// the author's own\n"
	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(implementation), 0o600); err != nil {
		t.Fatalf("writing the handler: %v", err)
	}
	if err := generate.Scaffold(runnable, dir); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if err := generate.GenerateForRelease(runnable, dir, release()); err != nil {
		t.Fatalf("GenerateForRelease: %v", err)
	}

	for _, name := range []string{
		generate.BindingsFile,
		generate.HarnessFile,
		filepath.Join(generate.CommandDirectory, generate.CommandFile),
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("generation did not write %s: %v", name, err)
		}
	}
	handler, err := os.ReadFile(filepath.Join(dir, "handler.go"))
	if err != nil {
		t.Fatalf("reading the handler: %v", err)
	}
	if string(handler) != implementation {
		t.Errorf("generation rewrote the author's handler:\n%s", handler)
	}
}

// The executable is a package of its own because the harness shares the
// author's, and it reaches the harness through the module the author's package
// is declared as.
func TestTheCommandRunsTheHarnessOfItsModule(t *testing.T) {
	source, err := generate.Command(packagePkg)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	for _, declared := range []string{"package main", fmt.Sprintf("%q", packagePkg), packagePkg + ".Main()"} {
		if !strings.Contains(string(source), declared) {
			t.Errorf("the command does not carry %s:\n%s", declared, source)
		}
	}
}

// A declaration the generator cannot render is refused before anything reaches
// disk, so a rejected runnable is not left half generated.
func TestGenerationRefusesADeclarationItCannotRender(t *testing.T) {
	runnable := declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute)
	runnable.Contract.Input.Fields = []*resources.RunnableField{{Name: "text", Type: "decimal"}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package wordcount\n"), 0o600); err != nil {
		t.Fatalf("writing the handler: %v", err)
	}

	if err := generate.GenerateForRelease(runnable, dir, release()); err == nil {
		t.Fatal("a type outside the bounded profile generated without error")
	}
	for _, name := range []string{generate.BindingsFile, generate.HarnessFile, generate.CommandDirectory} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("a refused declaration left %s behind", name)
		}
	}
}
