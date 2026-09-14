// Package agent is the gRPC surface of the Go Runnable agent: what the process
// serves and what it tells the CLI it can do.
//
// The agent advertises a capability only when this process registers the server
// behind it. It advertises BUILDER and no RUNTIME: a Runnable's invocation
// process is supervised by its caller, not by this agent.
//
// Within the Builder, a phase this agent cannot yet perform answers UNSUPPORTED
// in band rather than gRPC Unimplemented, which is the contract's own word for
// it and keeps one refusal shape across the service.
package agent

import (
	"context"
	"os/exec"

	"github.com/codefly-dev/core/agents"
	"github.com/codefly-dev/core/agents/services"
	agentv0 "github.com/codefly-dev/core/generated/go/codefly/services/agent/v0"
	"github.com/codefly-dev/core/resources"
	runners "github.com/codefly-dev/core/runners/base"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/codefly-dev/runnable-go/pkg/generate"
)

// readMe is what a human or model reads to decide what it may ask of this
// process, so it states the current surface rather than the roadmap.
const readMe = `Codefly Runnable agent for Go: typed handler scaffolding and typed ` +
	`bindings for the bounded contract profile, generated into the runnable's own ` +
	`Go module, through Builder gRPC.

The ` + resources.RunnableProtocolV1 + ` harness, native packaging and build ` +
	`evidence are not implemented: Builder.RunnableBuildInputs and Builder.Package ` +
	`report UNSUPPORTED. Generated files require ` + generate.GoRequirement + `.`

// Server implements the Agent service.
//
// UnimplementedAgentServer is embedded so an RPC added to the contract later
// answers Unimplemented instead of failing the build: an agent that has not
// opted into a new lifecycle feature must refuse it, not half-serve it.
// GetEffectiveInputs is deliberately left to that default — the advertisement
// carries no effective_inputs_versions, and the contract reads an absent
// version list as "no discovery, no persistent result reuse".
type Server struct {
	agentv0.UnimplementedAgentServer

	agent *resources.Agent
}

// New returns the Agent service bound to the agent's own manifest.
func New(agent *resources.Agent) *Server {
	return &Server{agent: agent}
}

// Manifest returns the agent's own identity, which a declaration pins and every
// build records.
func (s *Server) Manifest() *resources.Agent {
	return s.agent
}

// Registration is the complete gRPC surface this agent process serves. main
// passes it to agents.Serve, and the package's tests assert it against the
// advertised capabilities, so the two cannot drift apart.
func Registration(manifest *resources.Agent) agents.PluginRegistration {
	return agents.PluginRegistration{
		Agent:   New(manifest),
		Builder: NewBuilder(manifest),
	}
}

// GetAgentInformation describes the agent to the CLI.
//
// CapabilityOnly suppresses the advertisement helper's BUILDER+RUNTIME pair so
// BUILDER can be advertised on its own: pairing them would promise a Runtime
// this agent does not serve, and services.Instance would then dial one.
//
// The response is built per call rather than shared from a package value: gRPC
// serves concurrently and a protobuf message is mutable, so one returned
// pointer handed to several callers is a data race on the wire encoder.
func (s *Server) GetAgentInformation(_ context.Context, _ *agentv0.AgentInformationRequest) (*agentv0.AgentInformation, error) {
	info := services.Advertisement{
		CapabilityOnly: true,
		Backends: runners.BackendSupport{
			// What is advertised is exactly what generation and, later,
			// compilation require: a Go toolchain on this host. Image builds
			// stay recipe-only in the agent, so no Docker backend is claimed.
			Local:  func() bool { _, err := exec.LookPath("go"); return err == nil },
			Docker: false,
		},
		Toolchains: []agentv0.Toolchain_Type{agentv0.Toolchain_GO},
		Languages:  []agentv0.Language_Type{agentv0.Language_GO},
		ReadMe:     readMe,
	}.Build()
	info.Capabilities = append(info.Capabilities, &agentv0.Capability{Type: agentv0.Capability_BUILDER})
	return info, nil
}

// commands are the plugin commands this agent provides. It is the one list
// ListCommands answers from, so a command becomes visible to callers only by
// being added here — and TestEveryListedCommandRuns then requires
// RunPluginCommand to know it.
//
// There are none: everything this agent does happens through the runnable
// lifecycle, not through plugin commands.
func commands() []*agentv0.CommandDefinition {
	return nil
}

// ListCommands returns the plugin commands the agent provides.
func (s *Server) ListCommands(_ context.Context, _ *agentv0.ListCommandsRequest) (*agentv0.ListCommandsResponse, error) {
	return &agentv0.ListCommandsResponse{Commands: commands()}, nil
}

// RunPluginCommand executes one listed command.
//
// The agent lists none, so every name is one it does not provide and is refused
// with NotFound — the caller named something absent, which is distinct from a
// command that ran and failed.
func (s *Server) RunPluginCommand(_ context.Context, req *agentv0.RunPluginCommandRequest) (*agentv0.RunPluginCommandResponse, error) {
	return nil, status.Errorf(codes.NotFound, "agent provides no command %q", req.GetCommand())
}
