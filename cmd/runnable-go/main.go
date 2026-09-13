// Command runnable-go is the Go Runnable agent process.
//
// The executable name is the distribution identity the CLI resolves a runnable
// agent named "go" by, so the directory name is load-bearing: building this
// package produces a binary called runnable-go. TestAgentDeclaresRunnableGoIdentity
// pins that name against the agent manifest.
//
// The process owns no lifecycle of its own. agents.Serve binds the listener,
// installs the auth, panic-recovery and principal interceptors, registers the
// health service, writes the stdout handshake and handles shutdown; the agent
// contributes only the servers it can actually answer.
package main

import (
	"github.com/codefly-dev/core/agents"
	"github.com/codefly-dev/runnable-go/pkg/agent"
)

func main() {
	agents.Serve(agent.Registration())
}
