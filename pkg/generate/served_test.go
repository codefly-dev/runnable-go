package generate_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"
	"github.com/codefly-dev/core/resources"
	corerunnable "github.com/codefly-dev/core/runnable"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A generated harness serves; it is not launched. These tests start the real
// built binary, call it over HTTP exactly as the runtime's invoker does — core's
// header spellings, core's procedure, the bounded input as the whole body — and
// classify what came back with core's own ClassifyServed. Nothing here restates
// the contract: every constant is read from core, so a harness that drifted from
// it would fail here rather than in a live composition.

const workContext = "eyJ0eXAiOiJjb2RlZmx5LndvcmstY29udGV4dC92MSJ9.signed"

// served is a running generated harness.
type served struct {
	built   compiled
	address string
	stderr  *strings.Builder
}

// serve starts the built binary and waits until it answers. The port is chosen
// by the operating system and handed to the harness the way a placement hands
// it one: nothing here picks a number.
func serve(t *testing.T, built compiled) served {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}

	stderr := &strings.Builder{}
	process := exec.Command(built.binary)
	process.Env = []string{corerunnable.ListenAddressEnv + "=" + address}
	process.Stderr = stderr
	if err := process.Start(); err != nil {
		t.Fatalf("starting the runnable: %v", err)
	}
	t.Cleanup(func() {
		_ = process.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = process.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = process.Process.Kill()
		}
	})

	for range 400 {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return served{built: built, address: address, stderr: stderr}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("the runnable never answered on %s\n--- stderr ---\n%s", address, stderr.String())
	return served{}
}

// call sends one call the way the runtime's invoker sends one, and classifies
// the answer with core.
func (s served) call(t *testing.T, procedure string, payload string, headers map[string]string) *basev0.RunnableServedCompletion {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, "http://"+s.address+procedure, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("building the call: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(corerunnable.WorkContextHeader, workContext)
	for key, value := range headers {
		if value == "" {
			request.Header.Del(key)
			continue
		}
		request.Header.Set(key, value)
	}

	observed := corerunnable.Call{CalledAt: time.Now().UTC(), AuthorityResolved: true}
	response, err := (&http.Client{Timeout: 2 * time.Minute}).Do(request)
	observed.AnsweredAt = time.Now().UTC()
	if err != nil {
		observed.Trouble = err
	} else {
		defer response.Body.Close()
		observed.Answered = true
		observed.FailureCode = response.Header.Get(corerunnable.FailureCodeHeader)
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatalf("reading the answer: %v", err)
		}
		observed.Response = s.resultOf(t, response.StatusCode, string(body))
		if response.StatusCode != http.StatusOK && observed.FailureCode == "" {
			observed.Trouble = fmt.Errorf("owner answered %d: %s", response.StatusCode, body)
		}
	}
	completion, err := corerunnable.ClassifyServed(s.invocation(t), s.built.pkg, observed)
	if err != nil {
		t.Fatalf("ClassifyServed: %v", err)
	}
	return completion
}

// resultOf turns the owner's answer into the RunnableResult core classifies.
// A 200 is the bounded output document; anything else answered no output at
// all, and a result invented for it would be a completion the owner never gave.
func (s served) resultOf(t *testing.T, status int, body string) []byte {
	t.Helper()
	if status != http.StatusOK {
		return nil
	}
	result := &basev0.RunnableResult{
		Protocol:     resources.RunnableServedProtocolV1,
		InvocationId: "inv-1",
		Status:       basev0.RunnableResult_SUCCEEDED,
		Output:       []byte(body),
	}
	return resultDocument(t, result)
}

func (s served) invocation(t *testing.T) *basev0.RunnableInvocation {
	t.Helper()
	issued := time.Now().UTC()
	return &basev0.RunnableInvocation{
		Protocol:     resources.RunnableServedProtocolV1,
		Runnable:     release(),
		InvocationId: "inv-1",
		IntentId:     "intent-1",
		EffectId:     "effect-1",
		IssuedAt:     timestamppb.New(issued),
		Deadline:     timestamppb.New(issued.Add(time.Minute)),
		Input:        []byte(`{"text":"a b","tags":[],"note":null}`),
		Identity: &basev0.RunnableInvocationIdentity{
			Carrier: &basev0.RunnableInvocationIdentity_WorkContext{WorkContext: workContext},
		},
	}
}

// echoHandler counts the words of text and echoes it back.
const echoHandler = `package wordcount

import (
	"context"
	"errors"
	"strings"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	if in.Text == "" {
		return Output{}, Fail("empty_text", "text has no words")
	}
	if in.Text == "untyped" {
		return Output{}, errors.New("something went wrong")
	}
	if in.Text == "panic" {
		panic("the handler gave up")
	}
	if in.Text == "slow" {
		<-ctx.Done()
		return Output{Total: 0, Echo: "never"}, nil
	}
	return Output{Total: int64(len(strings.Fields(in.Text))), Echo: in.Text}, nil
}
`

func TestAGeneratedRunnableAnswersACall(t *testing.T) {
	harness := serve(t, compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler))

	completion := harness.call(t, corerunnable.ServedInvokeProcedure,
		`{"text":"the quick brown fox","tags":[],"note":null}`, nil)
	if completion.GetOutcome() != basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatalf("outcome %s, message %q\n--- stderr ---\n%s",
			completion.GetOutcome(), completion.GetMessage(), harness.stderr.String())
	}
	if !corerunnable.ServedOutcomeIsCertain(completion.GetOutcome()) {
		t.Fatal("a validated answer must prove the effect committed")
	}
	if got := string(completion.GetResult().GetOutput()); !strings.Contains(got, `"total":4`) {
		t.Fatalf("output %q does not carry the handler's count", got)
	}

	// The harness serves; it does not exit after one call. A second call
	// proves it, which is the whole difference from the placement this
	// replaced.
	second := harness.call(t, corerunnable.ServedInvokeProcedure, `{"text":"one","tags":[],"note":null}`, nil)
	if second.GetOutcome() != basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatalf("the second call did not succeed: %s %q", second.GetOutcome(), second.GetMessage())
	}
}

// TestOnlyTheHandlersOwnFailureIsProven is the property the whole taxonomy
// rests on. The failure-code header, and nothing else, tells a caller that no
// effect committed — so a harness that set it for a panic, an untyped error or
// a deadline would be asserting on the handler's behalf that its effect did not
// happen, and a caller would stop looking for a receipt that exists.
func TestOnlyTheHandlersOwnFailureIsProven(t *testing.T) {
	harness := serve(t, compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler))

	declared := harness.call(t, corerunnable.ServedInvokeProcedure, `{"text":"","tags":[],"note":null}`, nil)
	if declared.GetOutcome() != basev0.RunnableServedOutcome_SERVED_OWNER_FAILED {
		t.Fatalf("a declared Failure must be SERVED_OWNER_FAILED, got %s (%q)", declared.GetOutcome(), declared.GetMessage())
	}
	if declared.GetFailureCode() != "empty_text" {
		t.Fatalf("the operation's own code must travel verbatim, got %q", declared.GetFailureCode())
	}
	if !corerunnable.ServedOutcomeIsCertain(declared.GetOutcome()) {
		t.Fatal("a declared failure proves no effect committed")
	}

	for _, tc := range []struct{ name, text string }{
		{"an untyped error", "untyped"},
		{"a panic", "panic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completion := harness.call(t, corerunnable.ServedInvokeProcedure,
				`{"text":"`+tc.text+`","tags":[],"note":null}`, nil)
			if corerunnable.ServedOutcomeIsCertain(completion.GetOutcome()) {
				t.Fatalf("%s must leave the effect unproven, got %s", tc.name, completion.GetOutcome())
			}
			if completion.GetFailureCode() != "" {
				t.Fatalf("%s must not carry a failure code, got %q", tc.name, completion.GetFailureCode())
			}
		})
	}
}

// A call with no Work Context is the one thing the contract exists to prevent.
// The harness refuses it rather than running under whatever identity its own
// process happens to have, which is the state the required identity slot
// removed.
func TestACallWithNoWorkContextIsRefused(t *testing.T) {
	harness := serve(t, compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler))

	completion := harness.call(t, corerunnable.ServedInvokeProcedure,
		`{"text":"a b","tags":[],"note":null}`, map[string]string{corerunnable.WorkContextHeader: ""})
	if corerunnable.ServedOutcomeIsCertain(completion.GetOutcome()) {
		t.Fatalf("a refused call must not be reported as a proven outcome, got %s", completion.GetOutcome())
	}
	if !strings.Contains(harness.stderr.String()+completion.GetMessage(), "Work Context") {
		t.Fatalf("the refusal must name the missing Work Context; message %q stderr %q",
			completion.GetMessage(), harness.stderr.String())
	}
}

// The payload is checked against the installed contract before the handler
// runs, so a handler never sees a body the contract forbids.
func TestThePayloadIsCheckedBeforeTheHandlerRuns(t *testing.T) {
	harness := serve(t, compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler))

	for _, tc := range []struct{ name, payload string }{
		{"a missing required field", `{"tags":[],"note":null}`},
		{"an undeclared field", `{"text":"a","tags":[],"note":null,"extra":1}`},
		{"the wrong value type", `{"text":1,"tags":[],"note":null}`},
		{"a null where none is declared", `{"text":null,"tags":[],"note":null}`},
		{"a body that is not an object", `["text"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completion := harness.call(t, corerunnable.ServedInvokeProcedure, tc.payload, nil)
			if completion.GetOutcome() == basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
				t.Fatalf("%s was accepted", tc.name)
			}
			if completion.GetFailureCode() != "" {
				t.Fatalf("a refused payload is not the operation's own failure, got %q", completion.GetFailureCode())
			}
		})
	}
}

// The deadline header bounds the call, and a handler still running when it
// passes leaves the effect unproven rather than failed.
func TestTheDeadlineHeaderBoundsTheCall(t *testing.T) {
	harness := serve(t, compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), echoHandler))

	deadline := time.Now().UTC().Add(2 * time.Second)
	completion := harness.call(t, corerunnable.ServedInvokeProcedure, `{"text":"slow","tags":[],"note":null}`,
		map[string]string{corerunnable.DeadlineHeader: deadline.Format(corerunnable.DeadlineFormat)})
	if completion.GetOutcome() == basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatal("a handler that ran past the deadline must not report success")
	}
	if corerunnable.ServedOutcomeIsCertain(completion.GetOutcome()) {
		t.Fatalf("a deadline leaves the effect unproven, got %s", completion.GetOutcome())
	}

	// A deadline already in the past is refused before the handler runs:
	// spending the attempt would make a timeout look like work that happened.
	past := harness.call(t, corerunnable.ServedInvokeProcedure, `{"text":"a b","tags":[],"note":null}`,
		map[string]string{corerunnable.DeadlineHeader: time.Now().UTC().Add(-time.Minute).Format(corerunnable.DeadlineFormat)})
	if past.GetOutcome() == basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatal("a call whose deadline had passed must not run")
	}
}

// A receipt-recovery operation serves the receipt route, and an absent receipt
// answers without being an error: absent is "not yet known", never "no".
func TestTheReceiptRouteAnswersAbsentWithoutAnError(t *testing.T) {
	handler := echoHandler + `

func ReceiptOf(ctx context.Context, in Input) (Receipt, error) {
	if in.Text == "committed" {
		return Receipt{Output: Output{Total: 7, Echo: in.Text}, Found: true}, nil
	}
	return Receipt{}, nil
}
`
	harness := serve(t, compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryReceipt), handler))

	found := harness.call(t, corerunnable.ServedLookupProcedure, `{"text":"committed","tags":[],"note":null}`,
		map[string]string{corerunnable.EffectHeader: "effect-1"})
	if found.GetOutcome() != basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatalf("a found receipt answers the operation's own output, got %s (%q)", found.GetOutcome(), found.GetMessage())
	}

	absent := harness.call(t, corerunnable.ServedLookupProcedure, `{"text":"never","tags":[],"note":null}`,
		map[string]string{corerunnable.EffectHeader: "effect-2"})
	if absent.GetOutcome() == basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatal("an absent receipt is not a committed effect")
	}
	if corerunnable.ServedOutcomeIsCertain(absent.GetOutcome()) {
		t.Fatalf("an absent receipt is inconclusive, never proof; got %s", absent.GetOutcome())
	}

	// An effect identity is what a receipt is keyed by, so a receipt-recovery
	// call carrying none is refused rather than run.
	none := harness.call(t, corerunnable.ServedInvokeProcedure, `{"text":"a b","tags":[],"note":null}`,
		map[string]string{corerunnable.EffectHeader: ""})
	if none.GetOutcome() == basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatal("a receipt-recovery call with no effect identity must be refused")
	}
}

// The handler sees the caller's identity, which is the only thing a handler may
// do with a Work Context: forward it. Parsing or reconstructing one is not the
// harness's business and it never does either.
func TestTheHandlerSeesTheCallersIdentity(t *testing.T) {
	handler := `package wordcount

import (
	"context"
	"strings"
)

func Handle(ctx context.Context, in Input) (Output, error) {
	invocation, ok := InvocationFrom(ctx)
	if !ok {
		return Output{}, Fail("no_invocation", "the handler saw no invocation")
	}
	return Output{
		Total: int64(len(strings.Fields(in.Text))),
		Echo:  invocation.WorkContext + "|" + invocation.EffectID + "|" + invocation.Release.Name,
	}, nil
}
`
	harness := serve(t, compile(t, declaration(t, resources.RunnableCancellationNone, resources.RunnableRecoveryRecompute), handler))
	completion := harness.call(t, corerunnable.ServedInvokeProcedure, `{"text":"a b","tags":[],"note":null}`,
		map[string]string{corerunnable.EffectHeader: "effect-9"})
	if completion.GetOutcome() != basev0.RunnableServedOutcome_SERVED_SUCCEEDED {
		t.Fatalf("outcome %s (%q)", completion.GetOutcome(), completion.GetMessage())
	}
	got := string(completion.GetResult().GetOutput())
	for _, want := range []string{workContext, "effect-9", releaseName} {
		if !strings.Contains(got, want) {
			t.Fatalf("the handler did not see %q; output %s", want, got)
		}
	}
}
