package agent_test

import (
	"context"
	"strings"
	"testing"

	agentv0 "github.com/codefly-dev/core/generated/go/codefly/services/agent/v0"
	"github.com/codefly-dev/runnable-go/pkg/agent"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func information(t *testing.T) *agentv0.AgentInformation {
	t.Helper()
	info, err := agent.New().GetAgentInformation(context.Background(), &agentv0.AgentInformationRequest{})
	if err != nil {
		t.Fatalf("GetAgentInformation: %v", err)
	}
	return info
}

// An advertised capability is a promise the CLI acts on: services.Instance
// gates LoadBuilder and LoadRuntime on CheckCapabilities and then dials the
// matching service. Advertising one this process does not register turns that
// into an Unimplemented "unknown service" at the dial; registering one it does
// not advertise makes the server unreachable. Both directions are checked here
// so the two halves cannot drift apart as capabilities are added.
func TestAdvertisedCapabilitiesMatchRegisteredServers(t *testing.T) {
	registration := agent.Registration()
	if registration.Agent == nil {
		t.Fatal("registration serves no Agent: the CLI cannot ask the process what it supports")
	}

	info, err := registration.Agent.GetAgentInformation(context.Background(), &agentv0.AgentInformationRequest{})
	if err != nil {
		t.Fatalf("GetAgentInformation: %v", err)
	}

	advertised := make(map[agentv0.Capability_Type]bool, len(info.GetCapabilities()))
	for _, capability := range info.GetCapabilities() {
		if capability.GetType() == agentv0.Capability_UNKNOWN {
			t.Error("capability UNKNOWN is advertised: a capability must name what it promises")
		}
		advertised[capability.GetType()] = true
	}

	// HOT_RELOAD is a property of the Runtime lifecycle rather than a service
	// of its own, so it is served by the same registration as RUNTIME.
	served := map[agentv0.Capability_Type]bool{
		agentv0.Capability_BUILDER:            registration.Builder != nil,
		agentv0.Capability_RUNTIME:            registration.Runtime != nil,
		agentv0.Capability_HOT_RELOAD:         registration.Runtime != nil,
		agentv0.Capability_EXECUTION_EXPORTER: registration.ExecutionExporter != nil,
	}
	for capability, isServed := range served {
		if advertised[capability] && !isServed {
			t.Errorf("capability %v is advertised but no server is registered for it", capability)
		}
	}

	// The reverse: a registered server the agent never advertises is
	// unreachable, because the CLI refuses the phase before dialing.
	for capability, isServed := range map[agentv0.Capability_Type]bool{
		agentv0.Capability_BUILDER:            registration.Builder != nil,
		agentv0.Capability_RUNTIME:            registration.Runtime != nil,
		agentv0.Capability_EXECUTION_EXPORTER: registration.ExecutionExporter != nil,
	} {
		if isServed && !advertised[capability] {
			t.Errorf("a server is registered for %v but the capability is not advertised", capability)
		}
	}
}

// The Builder handoff is what issue #2 is blocked on: Builder.LoadRequest
// carries a ServiceIdentity and no RunnableIdentity, and Builder.CreateRequest
// carries nothing at all, so a Builder registered now could not know which
// runnable it was loaded for. Until core freezes that, the honest answer is to
// advertise nothing. This test is expected to change when the handoff lands.
func TestNoCapabilityIsAdvertisedWhileTheBuilderHandoffIsUndefined(t *testing.T) {
	if got := information(t).GetCapabilities(); len(got) != 0 {
		t.Fatalf("capabilities = %v, want none until a lifecycle server is registered", got)
	}
}

func TestAgentAdvertisesGo(t *testing.T) {
	languages := information(t).GetLanguages()
	if len(languages) != 1 {
		t.Fatalf("languages = %v, want exactly Go", languages)
	}
	if got := languages[0].GetType(); got != agentv0.Language_GO {
		t.Fatalf("language = %v, want GO", got)
	}
}

// read_me is what a human or model reads to decide what it may ask of the
// process, so it has to state the current surface rather than an aspiration.
func TestAgentDocumentsItsCurrentSurface(t *testing.T) {
	readMe := information(t).GetReadMe()
	if strings.TrimSpace(readMe) == "" {
		t.Fatal("read_me is empty: a caller has nothing to read about the agent")
	}
	if !strings.Contains(readMe, "Supported capabilities: none") {
		t.Errorf("read_me does not state the unsupported state:\n%s", readMe)
	}
}

// GetAgentInformation is served concurrently by gRPC. Returning a shared
// package-level message would hand the same mutable protobuf to every caller,
// which races in the wire encoder; each call must build its own.
func TestAgentInformationIsNotSharedBetweenCalls(t *testing.T) {
	first, second := information(t), information(t)
	if first == second {
		t.Fatal("GetAgentInformation returns one shared message: concurrent callers would race on it")
	}
	if len(first.GetLanguages()) == 0 || len(second.GetLanguages()) == 0 {
		t.Fatal("languages missing from a response")
	}
	if first.GetLanguages()[0] == second.GetLanguages()[0] {
		t.Fatal("GetAgentInformation shares nested messages between calls")
	}
}

// ListCommands and RunPluginCommand read one list, so a command can never be
// listed without being runnable or runnable without being listed. With no
// commands this reduces to refusing everything, and it keeps holding when
// commands are added.
func TestEveryListedCommandRuns(t *testing.T) {
	server := agent.New()
	listed, err := server.ListCommands(context.Background(), &agentv0.ListCommandsRequest{})
	if err != nil {
		t.Fatalf("ListCommands: %v", err)
	}
	for _, command := range listed.GetCommands() {
		_, err := server.RunPluginCommand(context.Background(), &agentv0.RunPluginCommandRequest{Command: command.GetName()})
		if status.Code(err) == codes.NotFound {
			t.Errorf("command %q is listed but RunPluginCommand does not know it", command.GetName())
		}
	}
}

func TestUnlistedCommandIsRefused(t *testing.T) {
	_, err := agent.New().RunPluginCommand(context.Background(), &agentv0.RunPluginCommandRequest{Command: "package"})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("RunPluginCommand code = %v, want NotFound", got)
	}
}

// AgentInformation advertises no effective_inputs_versions, which the contract
// reads as "no discovery and no persistent result reuse". Answering the RPC
// anyway would contradict that, so it must stay refused while the version list
// is empty.
func TestEffectiveInputsIsRefusedWhileNoVersionIsAdvertised(t *testing.T) {
	if versions := information(t).GetEffectiveInputsVersions(); len(versions) != 0 {
		t.Skipf("agent now advertises effective input versions %v; implement the RPC", versions)
	}
	_, err := agent.New().GetEffectiveInputs(context.Background(), &agentv0.GetEffectiveInputsRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("GetEffectiveInputs code = %v, want Unimplemented", got)
	}
}
