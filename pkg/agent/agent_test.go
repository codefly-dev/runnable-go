package agent_test

import (
	"context"
	"strings"
	"testing"

	agentv0 "github.com/codefly-dev/core/generated/go/codefly/services/agent/v0"
	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/runnable-go/pkg/agent"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// manifest is the agent's own identity, which a runnable declaration pins and
// Builder.Load compares a declaration against.
func manifest() *resources.Agent {
	return &resources.Agent{
		Kind:      resources.RunnableAgent,
		Name:      "go",
		Publisher: "codefly.dev",
		Version:   "0.0.1",
	}
}

func information(t *testing.T) *agentv0.AgentInformation {
	t.Helper()
	info, err := agent.New(manifest()).GetAgentInformation(context.Background(), &agentv0.AgentInformationRequest{})
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
	registration := agent.Registration(manifest())
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

// A Runnable's invocation process is supervised by its caller, so this agent
// serves no Runtime. The advertisement helper pairs BUILDER with RUNTIME by
// default, which is why BUILDER is advertised through CapabilityOnly and
// appended on its own; this pins that the pairing did not come back.
func TestAgentAdvertisesBuilderWithoutRuntime(t *testing.T) {
	var builder, runtime bool
	for _, capability := range information(t).GetCapabilities() {
		switch capability.GetType() {
		case agentv0.Capability_BUILDER:
			builder = true
		case agentv0.Capability_RUNTIME, agentv0.Capability_HOT_RELOAD:
			runtime = true
		}
	}
	if !builder {
		t.Error("BUILDER is not advertised, so the CLI refuses every Builder phase")
	}
	if runtime {
		t.Error("a Runtime capability is advertised, but this agent serves no Runtime")
	}
}

func TestAgentAdvertisesGo(t *testing.T) {
	info := information(t)
	languages := info.GetLanguages()
	if len(languages) != 1 || languages[0].GetType() != agentv0.Language_GO {
		t.Fatalf("languages = %v, want exactly GO", languages)
	}
	toolchains := info.GetToolchains()
	if len(toolchains) != 1 || toolchains[0].GetType() != agentv0.Toolchain_GO {
		t.Fatalf("toolchains = %v, want exactly GO", toolchains)
	}
}

// read_me is what a human or model reads to decide what it may ask of the
// process, so it has to name the phases that will refuse.
func TestAgentDocumentsItsUnsupportedPhases(t *testing.T) {
	readMe := information(t).GetReadMe()
	if strings.TrimSpace(readMe) == "" {
		t.Fatal("read_me is empty: a caller has nothing to read about the agent")
	}
	for _, phase := range []string{"RunnableBuildInputs", "Package", "UNSUPPORTED"} {
		if !strings.Contains(readMe, phase) {
			t.Errorf("read_me does not mention %q:\n%s", phase, readMe)
		}
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

// ListCommands and RunPluginCommand must agree: a command can never be listed
// without being runnable. With no commands this reduces to refusing everything,
// and it keeps holding when commands are added.
func TestEveryListedCommandRuns(t *testing.T) {
	server := agent.New(manifest())
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
	_, err := agent.New(manifest()).RunPluginCommand(context.Background(), &agentv0.RunPluginCommandRequest{Command: "package"})
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
	_, err := agent.New(manifest()).GetEffectiveInputs(context.Background(), &agentv0.GetEffectiveInputsRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("GetEffectiveInputs code = %v, want Unimplemented", got)
	}
}
