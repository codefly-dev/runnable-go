// Command runnable-go is the Codefly Runnable agent for Go.
//
// The executable name is the distribution identity the CLI resolves a runnable
// agent named "go" by. Building this package produces a binary named for the
// module's last path element, runnable-go, and TestAgentDeclaresRunnableGoIdentity
// pins that name against the agent manifest. The package sits at the repository
// root because the manifest it embeds does: go:embed cannot reach outside its
// own directory, and a second copy of the agent's identity could drift from the
// published one.
//
// The process owns no lifecycle of its own. agents.Serve binds the listener,
// installs the auth, panic-recovery and principal interceptors, registers the
// health service, writes the stdout handshake and handles shutdown; the agent
// contributes only the servers it can answer.
//
// The implementation lives under ./pkg:
//
//	github.com/codefly-dev/runnable-go/pkg/agent    — the Agent and Builder services
//	github.com/codefly-dev/runnable-go/pkg/generate — handler scaffold and typed bindings
package main

import (
	"embed"

	"github.com/codefly-dev/core/agents"
	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/core/shared"

	runnableagent "github.com/codefly-dev/runnable-go/pkg/agent"
)

// The manifest is embedded rather than read from disk: it is the agent's own
// identity, which a runnable declaration pins and Load compares against, so it
// must travel inside the executable that claims it.
//
//go:embed agent.codefly.yaml
var infoFS embed.FS

var manifest = shared.Must(resources.LoadFromFs[resources.Agent](shared.Embed(infoFS)))

func main() {
	agents.Serve(runnableagent.Registration(manifest.Of(resources.RunnableAgent)))
}
