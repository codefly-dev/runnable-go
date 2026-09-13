// Package agent is the Go Runnable agent's gRPC surface: what the process
// serves and what it tells the CLI it can do.
//
// The agent advertises a capability only when this process actually registers
// the server behind it. Today that set is empty. Every Builder phase a Runnable
// needs is driven from Builder.Load, whose LoadRequest carries a ServiceIdentity
// and no RunnableIdentity, so an agent process cannot learn which runnable it
// was loaded for; and Builder.Create carries no request fields at all. Until
// core freezes that handoff, registering a Builder here would advertise a
// capability whose every method would have to guess its subject.
//
// Advertising nothing is not a placeholder: services.Instance gates LoadBuilder
// and LoadRuntime on CheckCapabilities, so an honest empty set makes the CLI
// refuse the phase with a clear error instead of dialing an unknown service.
package agent

import (
	"context"

	"github.com/codefly-dev/core/agents"
	agentv0 "github.com/codefly-dev/core/generated/go/codefly/services/agent/v0"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// readMe is the human- and model-facing description returned in
// AgentInformation.read_me. It states the agent's current surface rather than
// the repository's roadmap: a caller reads this to decide what it may ask of
// the process now.
const readMe = `The Codefly Runnable agent for Go.

Serves the agent lifecycle: identity, capabilities and plugin commands.

Supported capabilities: none. Generating bindings, packaging a native
executable and running an invocation are Builder phases, and the Builder
contract does not yet carry the identity of the runnable being built. The
agent advertises those capabilities only once it can serve them.`

// Server implements the Agent service.
//
// UnimplementedAgentServer is embedded so an RPC added to the contract later
// answers Unimplemented instead of failing the build: an agent that has not
// opted into a new lifecycle feature must refuse it, not half-serve it.
// GetEffectiveInputs is deliberately left to that default — AgentInformation
// advertises no effective_inputs_versions, and the contract reads an absent
// version list as "no discovery, no persistent result reuse".
type Server struct {
	agentv0.UnimplementedAgentServer
}

// New returns the Agent service implementation.
func New() *Server {
	return &Server{}
}

// Registration is the complete gRPC surface this agent process serves. main
// passes it to agents.Serve, and the package's tests assert it against the
// advertised capabilities, so the two cannot drift apart.
func Registration() agents.PluginRegistration {
	return agents.PluginRegistration{Agent: New()}
}

// capabilities are the lifecycle capabilities this process serves. It is
// deliberately empty; see the package documentation.
func capabilities() []*agentv0.Capability {
	return nil
}

// commands are the plugin commands this agent provides. It is the one list
// ListCommands answers from, so a command becomes visible to callers only by
// being added here — and TestEveryListedCommandRuns then requires
// RunPluginCommand to know it.
func commands() []*agentv0.CommandDefinition {
	return nil
}

// GetAgentInformation describes the agent to the CLI.
//
// The response is built per call rather than shared from a package value:
// gRPC serves concurrently and a protobuf message is mutable, so one returned
// pointer handed to several callers is a data race on the wire encoder.
func (s *Server) GetAgentInformation(_ context.Context, _ *agentv0.AgentInformationRequest) (*agentv0.AgentInformation, error) {
	return &agentv0.AgentInformation{
		Capabilities: capabilities(),
		Languages:    []*agentv0.Language{{Type: agentv0.Language_GO}},
		ReadMe:       readMe,
	}, nil
}

// ListCommands returns the plugin commands the agent provides.
func (s *Server) ListCommands(_ context.Context, _ *agentv0.ListCommandsRequest) (*agentv0.ListCommandsResponse, error) {
	return &agentv0.ListCommandsResponse{Commands: commands()}, nil
}

// RunPluginCommand executes one listed command.
//
// The agent lists no commands, so every name is one it does not provide and is
// refused with NotFound — the caller named something absent, which is distinct
// from a command that ran and failed. Adding a command to commands() without
// adding its dispatch here fails TestEveryListedCommandRuns rather than
// silently answering NotFound for a command the agent advertises.
func (s *Server) RunPluginCommand(_ context.Context, req *agentv0.RunPluginCommandRequest) (*agentv0.RunPluginCommandResponse, error) {
	return nil, status.Errorf(codes.NotFound, "agent provides no command %q", req.GetCommand())
}
