package generate_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"
	"github.com/codefly-dev/core/resources"
	corerunnable "github.com/codefly-dev/core/runnable"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/codefly-dev/runnable-go/pkg/generate"
)

// The harness is qualified against core rather than against this package's own
// idea of the framing: every request is the document core's launcher writes,
// and every outcome is the one core's Complete classifies. A harness that
// agreed only with its own tests would agree with no launcher.

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
			Facilities:   []resources.RunnableFacility{resources.RunnableFacilityNative},
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

// compiled is a generated runnable built the way a launcher would receive it.
type compiled struct {
	runnable *resources.Runnable
	pkg      *basev0.RunnablePackage
	binary   string
}

// compile generates the runnable, writes the author's handler and builds the
// executable a launcher starts. The module resolves nothing from the network:
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
			Kind:      basev0.RunnableArtifact_NATIVE,
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

// invocationFor is the document a launcher writes for one payload.
func invocationFor(t *testing.T, payload string, budget time.Duration) *basev0.RunnableInvocation {
	t.Helper()
	issued := time.Now().UTC()
	return &basev0.RunnableInvocation{
		Protocol:     resources.RunnableProtocolV1,
		Runnable:     release(),
		InvocationId: "inv-1",
		IntentId:     "intent-1",
		IssuedAt:     timestamppb.New(issued),
		Deadline:     timestamppb.New(issued.Add(budget)),
		Input:        []byte(payload),
	}
}

// observed is what a launcher saw of one invocation.
type observed struct {
	exit       int
	signal     string
	stdout     string
	stderr     string
	result     []byte
	present    bool
	resultMode os.FileMode
	completion *basev0.RunnableCompletion
}

// launch runs the invocation the way core's launcher does: the encoded document
// on disk, the framing in the environment, and the process observed rather than
// interpreted. ended is how the launcher itself ended the process.
func launch(t *testing.T, built compiled, inv *basev0.RunnableInvocation, ended corerunnable.EndReason, interrupt bool) observed {
	t.Helper()
	prepared, err := corerunnable.PrepareInvocation(inv, built.pkg)
	if err != nil {
		t.Fatalf("PrepareInvocation: %v", err)
	}
	document, err := corerunnable.EncodeInvocation(prepared)
	if err != nil {
		t.Fatalf("EncodeInvocation: %v", err)
	}
	dir := t.TempDir()
	invocationPath := filepath.Join(dir, "invocation.json")
	resultPath := filepath.Join(dir, "result.json")
	if err := os.WriteFile(invocationPath, document, 0o600); err != nil {
		t.Fatalf("writing the invocation: %v", err)
	}
	return runProcess(t, built, prepared, invocationPath, resultPath,
		corerunnable.InvocationEnvironment(prepared, invocationPath, resultPath), ended, interrupt)
}

// launchRaw runs a document core would never write, to prove the harness
// refuses it rather than trusting its launcher.
func launchRaw(t *testing.T, built compiled, document string, environment map[string]string) observed {
	t.Helper()
	dir := t.TempDir()
	invocationPath := filepath.Join(dir, "invocation.json")
	resultPath := filepath.Join(dir, "result.json")
	if err := os.WriteFile(invocationPath, []byte(document), 0o600); err != nil {
		t.Fatalf("writing the invocation: %v", err)
	}
	full := map[string]string{
		"CODEFLY__RUNNABLE_PROTOCOL":   resources.RunnableProtocolV1,
		"CODEFLY__RUNNABLE_INVOCATION": invocationPath,
		"CODEFLY__RUNNABLE_RESULT":     resultPath,
	}
	for key, value := range environment {
		if value == "" {
			delete(full, key)
			continue
		}
		full[key] = value
	}
	return runProcess(t, built, nil, invocationPath, resultPath, full, corerunnable.EndedOnItsOwn, false)
}

// readyMarker is the file a handler under test writes when it is running. The
// launcher signals on the marker rather than after a delay, so an interruption
// test proves what the harness does with a signal rather than how fast the
// process started.
const readyMarker = "started"

func runProcess(
	t *testing.T,
	built compiled,
	inv *basev0.RunnableInvocation,
	invocationPath string,
	resultPath string,
	environment map[string]string,
	ended corerunnable.EndReason,
	interrupt bool,
) observed {
	t.Helper()
	process := exec.Command(built.binary)
	process.Dir = filepath.Dir(invocationPath)
	process.Env = []string{}
	for key, value := range environment {
		process.Env = append(process.Env, key+"="+value)
	}
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	process.Stdout, process.Stderr = stdout, stderr

	start := time.Now().UTC()
	if err := process.Start(); err != nil {
		t.Fatalf("starting the runnable: %v", err)
	}
	if interrupt {
		signalled := make(chan struct{})
		go func() {
			defer close(signalled)
			marker := filepath.Join(process.Dir, readyMarker)
			for range 4000 {
				if _, err := os.Stat(marker); err == nil {
					_ = process.Process.Signal(syscall.SIGTERM)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
		defer func() { <-signalled }()
	}
	waitErr := process.Wait()
	end := time.Now().UTC()

	seen := observed{stdout: stdout.String(), stderr: stderr.String()}
	seen.exit = process.ProcessState.ExitCode()
	if status, ok := process.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		seen.signal = status.Signal().String()
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		t.Fatalf("running the runnable: %v", waitErr)
	}

	if info, err := os.Stat(resultPath); err == nil {
		seen.present = true
		seen.resultMode = info.Mode().Perm()
		if seen.result, err = os.ReadFile(resultPath); err != nil {
			t.Fatalf("reading the result: %v", err)
		}
	}
	if inv == nil {
		return seen
	}
	completion, err := corerunnable.Complete(inv, built.pkg, corerunnable.Observation{
		StartedAt:     start,
		EndedAt:       end,
		ResultPresent: seen.present,
		Result:        seen.result,
		ExitCode:      int32(seen.exit),
		Signal:        seen.signal,
		Ended:         ended,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	seen.completion = completion
	return seen
}

// echoHandler returns the text it was given, so the payload decides the output
// and a stale or fabricated result cannot pass for this invocation's.
const echoHandler = `package wordcount

import "context"

func Handle(ctx context.Context, in Input) (Output, error) {
	total := int64(len(in.Tags))
	if repeat, ok := in.Repeat.Get(); ok {
		total += repeat
	}
	return Output{Total: total, Echo: in.Text}, nil
}
`

func decodeOutput(t *testing.T, completion *basev0.RunnableCompletion) map[string]any {
	t.Helper()
	if completion.GetResult() == nil {
		t.Fatalf("completion carries no result: %s / %s", completion.GetOutcome(), completion.GetMessage())
	}
	var output map[string]any
	if err := json.Unmarshal(completion.GetResult().GetOutput(), &output); err != nil {
		t.Fatalf("decoding the output: %v", err)
	}
	return output
}

// A generated runnable answers a real invocation, and core recognizes the
// answer as this invocation's proven success.
func TestAGeneratedRunnableCompletesAnInvocation(t *testing.T) {
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler)

	seen := launch(t, built, invocationFor(t, `{"text":"hello","note":null,"tags":["a","b"],"repeat":5}`, time.Minute),
		corerunnable.EndedOnItsOwn, false)

	if seen.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", seen.exit, seen.stderr)
	}
	if got := seen.completion.GetOutcome(); got != basev0.RunnableCompletion_SUCCEEDED {
		t.Fatalf("outcome = %s (%s)", got, seen.completion.GetMessage())
	}
	output := decodeOutput(t, seen.completion)
	if output["echo"] != "hello" {
		t.Errorf("echo = %v, want the input text: the output does not depend on the payload", output["echo"])
	}
	if output["total"] != float64(7) {
		t.Errorf("total = %v, want 7", output["total"])
	}
	// The result is the launcher's to read; the facility decides which
	// accounts may.
	if seen.resultMode != 0o644 {
		t.Errorf("result mode = %v, want 0644", seen.resultMode)
	}
	if !corerunnable.OutcomeIsCertain(seen.completion.GetOutcome()) {
		t.Error("a validated success is a certain outcome")
	}
}

// The completion is data and the streams are diagnostics. A handler writing to
// both must not be able to put anything into the result document, nor the
// result onto a stream.
func TestLogsAreSeparateFromTheCompletion(t *testing.T) {
	const chatty = `package wordcount

import (
	"context"
	"fmt"
	"os"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	fmt.Println("stdout diagnostic")
	fmt.Fprintln(os.Stderr, "stderr diagnostic")
	return Output{Total: 1, Echo: in.Text}, nil
}
`
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), chatty)
	seen := launch(t, built, invocationFor(t, `{"text":"x","note":null,"tags":[]}`, time.Minute), corerunnable.EndedOnItsOwn, false)

	if seen.completion.GetOutcome() != basev0.RunnableCompletion_SUCCEEDED {
		t.Fatalf("outcome = %s (%s)", seen.completion.GetOutcome(), seen.completion.GetMessage())
	}
	if !strings.Contains(seen.stdout, "stdout diagnostic") || !strings.Contains(seen.stderr, "stderr diagnostic") {
		t.Errorf("a handler's diagnostics did not reach the streams\nstdout: %q\nstderr: %q", seen.stdout, seen.stderr)
	}
	if strings.Contains(seen.stdout, "\"protocol\"") {
		t.Errorf("the result document was written to stdout: %q", seen.stdout)
	}
	if strings.Contains(string(seen.result), "diagnostic") {
		t.Errorf("a diagnostic reached the completion: %s", seen.result)
	}
}

// The bindings carry value types and null states; they do not carry required
// keys, undeclared keys, the integer range or the refusal to coerce. Every one
// of those is the harness's, and each is an invalid input rather than a
// completion.
func TestThePayloadIsValidatedAgainstTheContract(t *testing.T) {
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler)

	for _, test := range []struct{ name, input, reason string }{
		{"a required key is absent", `{"note":null,"tags":[]}`, "input.text is required"},
		{"an undeclared key", `{"text":"a","note":null,"tags":[],"extra":1}`, "input.extra is not declared by the contract"},
		{"null for a key that is not nullable", `{"text":null,"note":null,"tags":[]}`, "input.text is null"},
		{"a fraction is not an integer", `{"text":"a","note":null,"tags":[],"repeat":3.0}`, "input.repeat must be an integer, got number"},
		{"a string is not an integer", `{"text":"a","note":null,"tags":[],"repeat":"3"}`, "input.repeat must be an integer, got string"},
		{"a boolean is not an integer", `{"text":"a","note":null,"tags":[],"repeat":true}`, "input.repeat must be an integer, got boolean"},
		{"an integer is not a string", `{"text":3,"note":null,"tags":[]}`, "input.text must be a string, got integer"},
		{"beyond signed 64 bits", `{"text":"a","note":null,"tags":[],"repeat":9223372036854775808}`, "outside the signed 64-bit integer range"},
		{"an element of an array", `{"text":"a","note":null,"tags":["a",2]}`, "input.tags[1] must be a string, got integer"},
		{"a nested object field", `{"text":"a","note":null,"tags":[],"options":{"limit":"x"}}`, "input.options.limit must be an integer"},
		{"an undeclared nested key", `{"text":"a","note":null,"tags":[],"options":{"limit":1,"other":2}}`, "input.options.other is not declared"},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen := launch(t, built, invocationFor(t, test.input, time.Minute), corerunnable.EndedOnItsOwn, false)
			if seen.exit != 64 {
				t.Fatalf("exit = %d, want 64\nstderr:\n%s", seen.exit, seen.stderr)
			}
			if seen.present {
				t.Errorf("a refused payload wrote a result: %s", seen.result)
			}
			if !strings.Contains(seen.stderr, test.reason) {
				t.Errorf("stderr does not name the reason %q:\n%s", test.reason, seen.stderr)
			}
		})
	}

	// The boundary itself is inside the profile, so the refusal above is the
	// range and not the digits.
	seen := launch(t, built, invocationFor(t, `{"text":"a","note":null,"tags":[],"repeat":9223372036854775807}`, time.Minute),
		corerunnable.EndedOnItsOwn, false)
	if seen.exit != 0 {
		t.Fatalf("the largest signed 64-bit integer was refused: exit %d\n%s", seen.exit, seen.stderr)
	}
}

// Framing this harness does not implement is refused before the handler runs,
// and nothing is bound to an invocation identity, so no result is written.
func TestInvalidFramingIsRefusedBeforeTheHandlerRuns(t *testing.T) {
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler)
	valid := `{"protocol":"codefly.runnable/v1","runnable":{"name":"word-count","module":"proof","workspace":"proof","version":"0.0.1"},` +
		`"invocation_id":"inv-1","intent_id":"intent-1","issued_at":"2026-09-13T10:00:00Z","deadline":"2026-09-13T10:01:00Z","input":%q}`
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"text":"a","note":null,"tags":[]}`))

	for _, test := range []struct {
		name        string
		document    string
		environment map[string]string
		reason      string
	}{
		{
			name:     "another release",
			document: strings.Replace(fmt.Sprintf(valid, encoded), `"version":"0.0.1"`, `"version":"9.0.0"`, 1),
			reason:   "another Runnable release",
		},
		{
			name:     "an undeclared request field",
			document: strings.Replace(fmt.Sprintf(valid, encoded), `"invocation_id"`, `"unknown":1,"invocation_id"`, 1),
			reason:   "undeclared field",
		},
		{
			name: "a duplicate JSON key",
			document: strings.Replace(fmt.Sprintf(valid, encoded), `"intent_id":"intent-1"`,
				`"intent_id":"intent-1","intent_id":"intent-2"`, 1),
			reason: "duplicate JSON key",
		},
		{
			name:     "a deadline that is not after the instant it was issued",
			document: strings.Replace(fmt.Sprintf(valid, encoded), `"deadline":"2026-09-13T10:01:00Z"`, `"deadline":"2026-09-13T10:00:00Z"`, 1),
			reason:   "must be after issued_at",
		},
		{
			name:     "an identifier beyond 128 characters",
			document: strings.Replace(fmt.Sprintf(valid, encoded), `"inv-1"`, `"`+strings.Repeat("i", 129)+`"`, 1),
			reason:   "invocation_id must contain 1 to 128 characters",
		},
		{
			name:     "an input that is not base64",
			document: fmt.Sprintf(valid, "not-base64!"),
			reason:   "not base64",
		},
		{
			name:     "an input that is not an object",
			document: fmt.Sprintf(valid, base64.StdEncoding.EncodeToString([]byte(`[1,2]`))),
			reason:   "must decode to one JSON object",
		},
		{
			name:        "another protocol",
			document:    fmt.Sprintf(valid, encoded),
			environment: map[string]string{"CODEFLY__RUNNABLE_PROTOCOL": "codefly.runnable/v2"},
			reason:      "not \"codefly.runnable/v1\"",
		},
		{
			name:        "no result path",
			document:    fmt.Sprintf(valid, encoded),
			environment: map[string]string{"CODEFLY__RUNNABLE_RESULT": ""},
			reason:      "CODEFLY__RUNNABLE_RESULT is required",
		},
		{
			name:        "a relative document path",
			document:    fmt.Sprintf(valid, encoded),
			environment: map[string]string{"CODEFLY__RUNNABLE_INVOCATION": "invocation.json"},
			reason:      "must be an absolute path",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen := launchRaw(t, built, test.document, test.environment)
			if seen.exit != 69 {
				t.Fatalf("exit = %d, want 69\nstderr:\n%s", seen.exit, seen.stderr)
			}
			if seen.present {
				t.Errorf("refused framing wrote a result: %s", seen.result)
			}
			if !strings.Contains(seen.stderr, test.reason) {
				t.Errorf("stderr does not name the reason %q:\n%s", test.reason, seen.stderr)
			}
		})
	}
}

// Only an explicit failure says what became of the effect. Everything else a
// handler can do leaves it unproven, and an unproven effect writes no result
// rather than one a caller would act on.
func TestOnlyAnExplicitFailureIsCertain(t *testing.T) {
	const failing = `package wordcount

import (
	"context"
	"errors"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	switch in.Text {
	case "refuse":
		return Output{}, Fail("unavailable", "the operation was refused")
	case "panic":
		panic("the handler panicked")
	}
	return Output{}, errors.New("something unexpected happened")
}
`
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), failing)

	refused := launch(t, built, invocationFor(t, `{"text":"refuse","note":null,"tags":[]}`, time.Minute), corerunnable.EndedOnItsOwn, false)
	if refused.exit != 66 {
		t.Fatalf("exit = %d, want 66\nstderr:\n%s", refused.exit, refused.stderr)
	}
	if got := refused.completion.GetOutcome(); got != basev0.RunnableCompletion_FAILED {
		t.Fatalf("outcome = %s, want FAILED (%s)", got, refused.completion.GetMessage())
	}
	if code := refused.completion.GetResult().GetError().GetCode(); code != "unavailable" {
		t.Errorf("failure code = %q, want the handler's own", code)
	}
	if !corerunnable.OutcomeIsCertain(refused.completion.GetOutcome()) {
		t.Error("an explicit failure is a certain outcome")
	}

	for _, test := range []struct{ name, text string }{
		{"an unexpected error", "boom"},
		{"a panic", "panic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen := launch(t, built, invocationFor(t, fmt.Sprintf(`{"text":%q,"note":null,"tags":[]}`, test.text), time.Minute),
				corerunnable.EndedOnItsOwn, false)
			if seen.present {
				t.Fatalf("an unproven effect wrote a result: %s", seen.result)
			}
			if seen.exit != 66 {
				t.Errorf("exit = %d, want 66\nstderr:\n%s", seen.exit, seen.stderr)
			}
			if corerunnable.OutcomeIsCertain(seen.completion.GetOutcome()) {
				t.Errorf("outcome %s is certain, but nothing proved what became of the effect", seen.completion.GetOutcome())
			}
		})
	}
}

// The budget is the caller's. A handler that outlives it does not get to report
// a completion, and the launcher's own deadline is what makes the outcome a
// timeout rather than a crash.
func TestTheDeadlineEndsTheInvocation(t *testing.T) {
	const slow = `package wordcount

import (
	"context"
	"time"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	time.Sleep(30 * time.Second)
	return Output{Total: 1, Echo: in.Text}, nil
}
`
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), slow)

	seen := launch(t, built, invocationFor(t, `{"text":"a","note":null,"tags":[]}`, 300*time.Millisecond),
		corerunnable.EndedOnDeadline, false)

	if seen.exit != 67 {
		t.Fatalf("exit = %d, want 67\nstderr:\n%s", seen.exit, seen.stderr)
	}
	if seen.present {
		t.Errorf("a timed-out invocation wrote a result: %s", seen.result)
	}
	if got := seen.completion.GetOutcome(); got != basev0.RunnableCompletion_TIMED_OUT {
		t.Errorf("outcome = %s, want TIMED_OUT (%s)", got, seen.completion.GetMessage())
	}
}

// A handler that swallows the deadline does not turn it into a completion: the
// reason the wait ended outlives whatever the handler returns afterwards.
func TestASwallowedDeadlineIsStillATimeout(t *testing.T) {
	const swallowing = `package wordcount

import (
	"context"
	"time"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)
	return Output{Total: 1, Echo: in.Text}, nil
}
`
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), swallowing)

	seen := launch(t, built, invocationFor(t, `{"text":"a","note":null,"tags":[]}`, 300*time.Millisecond),
		corerunnable.EndedOnDeadline, false)

	if seen.exit != 67 {
		t.Fatalf("exit = %d, want 67\nstderr:\n%s", seen.exit, seen.stderr)
	}
	if seen.present {
		t.Errorf("a swallowed deadline still reported a completion: %s", seen.result)
	}
}

// An interruption is reported only by a package that declares it can take one.
// A declaration claiming no cancellation installs no handler, so the process
// dies on the signal and the launcher sees that rather than a claim the harness
// cannot support.
func TestAnInterruptionIsReportedOnlyWhereDeclared(t *testing.T) {
	const waiting = `package wordcount

import (
	"context"
	"os"
	"time"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	if err := os.WriteFile("started", []byte("x"), 0o600); err != nil {
		return Output{}, err
	}
	time.Sleep(30 * time.Second)
	return Output{Total: 1, Echo: in.Text}, nil
}
`
	t.Run("declared", func(t *testing.T) {
		built := compile(t, declaration(t, resources.RunnableCancellationSignal, resources.RunnableRecoveryRecompute), waiting)
		seen := launch(t, built, invocationFor(t, `{"text":"a","note":null,"tags":[]}`, time.Minute),
			corerunnable.EndedOnCancel, true)

		if seen.exit != 68 {
			t.Fatalf("exit = %d, want 68\nstderr:\n%s", seen.exit, seen.stderr)
		}
		if !seen.present {
			t.Fatal("a handled interruption reported no result, so the launcher cannot tell it was handled")
		}
		if got := seen.completion.GetOutcome(); got != basev0.RunnableCompletion_CANCELED {
			t.Errorf("outcome = %s, want CANCELED (%s)", got, seen.completion.GetMessage())
		}
		// An interruption the harness handled is still an uncertain end: it
		// says the work stopped, not what became of the effect.
		if corerunnable.OutcomeIsCertain(seen.completion.GetOutcome()) {
			t.Error("a handled interruption is not a certain outcome")
		}
	})

	t.Run("not declared", func(t *testing.T) {
		built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), waiting)
		seen := launch(t, built, invocationFor(t, `{"text":"a","note":null,"tags":[]}`, time.Minute),
			corerunnable.EndedOnCancel, true)

		if seen.signal == "" {
			t.Fatalf("the process was not ended by the signal: exit %d", seen.exit)
		}
		if seen.present {
			t.Errorf("a package declaring no cancellation claimed a handled interruption: %s", seen.result)
		}
	})
}

// A result at the path is this invocation's or it is nothing. An earlier
// attempt's document left there would otherwise be read as this run's proven
// result, which is what makes a retried uncertain invocation dangerous.
func TestAnEarlierResultIsClearedBeforeTheRequestIsRead(t *testing.T) {
	const failing = `package wordcount

import (
	"context"
	"errors"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	return Output{}, errors.New("this attempt proves nothing")
}
`
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), failing)

	inv := invocationFor(t, `{"text":"a","note":null,"tags":[]}`, time.Minute)
	prepared, err := corerunnable.PrepareInvocation(inv, built.pkg)
	if err != nil {
		t.Fatalf("PrepareInvocation: %v", err)
	}
	document, err := corerunnable.EncodeInvocation(prepared)
	if err != nil {
		t.Fatalf("EncodeInvocation: %v", err)
	}
	dir := t.TempDir()
	invocationPath := filepath.Join(dir, "invocation.json")
	resultPath := filepath.Join(dir, "result.json")
	if err := os.WriteFile(invocationPath, document, 0o600); err != nil {
		t.Fatalf("writing the invocation: %v", err)
	}
	stale := `{"protocol":"codefly.runnable/v1","invocation_id":"inv-1","status":"SUCCEEDED","output":"e30="}`
	if err := os.WriteFile(resultPath, []byte(stale), 0o600); err != nil {
		t.Fatalf("writing the stale result: %v", err)
	}

	seen := runProcess(t, built, prepared, invocationPath, resultPath,
		corerunnable.InvocationEnvironment(prepared, invocationPath, resultPath), corerunnable.EndedOnItsOwn, false)

	if seen.present {
		t.Fatalf("an earlier attempt's result survived an outcome that proves nothing: %s", seen.result)
	}
	if got := seen.completion.GetOutcome(); corerunnable.OutcomeIsCertain(got) {
		t.Errorf("outcome = %s, which reads an earlier attempt's document as this run's", got)
	}
}

// The output bound is the declaration's, and an output over it is not a
// completion the launcher may record.
func TestAnOversizedOutputIsRefused(t *testing.T) {
	const verbose = `package wordcount

import (
	"context"
	"strings"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	return Output{Total: 1, Echo: strings.Repeat("x", 4096)}, nil
}
`
	runnable := declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute)
	runnable.Execution.Payload = &resources.RunnablePayload{MaxInputBytes: 65536, MaxOutputBytes: 256}
	built := compile(t, runnable, verbose)

	seen := launch(t, built, invocationFor(t, `{"text":"a","note":null,"tags":[]}`, time.Minute), corerunnable.EndedOnItsOwn, false)

	if seen.exit != 65 {
		t.Fatalf("exit = %d, want 65\nstderr:\n%s", seen.exit, seen.stderr)
	}
	if seen.present {
		t.Errorf("an oversized output was reported as a completion: %s", seen.result)
	}
	if !strings.Contains(seen.stderr, "over the declared 256 byte bound") {
		t.Errorf("stderr does not name the bound:\n%s", seen.stderr)
	}
}

// A package whose uncertain outcome is resolved by looking up an effect cannot
// run an invocation that carries no effect identity to look up.
func TestReceiptRecoveryRequiresAnEffectIdentity(t *testing.T) {
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryReceipt), echoHandler)

	document := `{"protocol":"codefly.runnable/v1","runnable":{"name":"word-count","module":"proof","workspace":"proof","version":"0.0.1"},` +
		`"invocation_id":"inv-1","intent_id":"intent-1","issued_at":"2026-09-13T10:00:00Z","deadline":"2026-09-13T10:01:00Z","input":"` +
		base64.StdEncoding.EncodeToString([]byte(`{"text":"a","note":null,"tags":[]}`)) + `"}`

	seen := launchRaw(t, built, document, nil)
	if seen.exit != 69 {
		t.Fatalf("exit = %d, want 69\nstderr:\n%s", seen.exit, seen.stderr)
	}
	if !strings.Contains(seen.stderr, "effect_id is required for receipt recovery") {
		t.Errorf("stderr does not name the reason:\n%s", seen.stderr)
	}
}

// The handler is given the caller's identity, not one it invents: an effect
// recorded under it is the one the caller looks up.
func TestTheHandlerSeesTheCallersInvocation(t *testing.T) {
	const reporting = `package wordcount

import (
	"context"
	"errors"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	invocation, ok := InvocationFrom(ctx)
	if !ok {
		return Output{}, errors.New("the handler was given no invocation")
	}
	if invocation.ID != "inv-1" || invocation.IntentID != "intent-1" {
		return Output{}, errors.New("the invocation identity is not the caller's: " + invocation.ID + "/" + invocation.IntentID)
	}
	if invocation.Release.Version != "0.0.1" || invocation.Release.Workspace != "proof" {
		return Output{}, errors.New("the release is not the one this package implements")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		return Output{}, errors.New("the handler was given no deadline")
	}
	return Output{Total: 1, Echo: invocation.ID}, nil
}
`
	built := compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), reporting)
	seen := launch(t, built, invocationFor(t, `{"text":"a","note":null,"tags":[]}`, time.Minute), corerunnable.EndedOnItsOwn, false)

	if seen.completion.GetOutcome() != basev0.RunnableCompletion_SUCCEEDED {
		t.Fatalf("outcome = %s (%s)\nstderr:\n%s", seen.completion.GetOutcome(), seen.completion.GetMessage(), seen.stderr)
	}
	if output := decodeOutput(t, seen.completion); output["echo"] != "inv-1" {
		t.Errorf("echo = %v, want the invocation identity", output["echo"])
	}
}
