package runnablego_test

import (
	"context"
	"testing"

	"github.com/codefly-dev/core/resources"
)

// The distribution identity is what the CLI resolves an agent by: a runnable
// agent named go is the runnable-go repository, release asset and executable.
func TestAgentDeclaresRunnableGoIdentity(t *testing.T) {
	agent, err := resources.LoadFromPath[resources.Agent](context.Background(), "agent.codefly.yaml")
	if err != nil {
		t.Fatalf("loading agent.codefly.yaml: %v", err)
	}
	if agent.Kind != resources.RunnableAgent {
		t.Fatalf("kind = %q, want %q", agent.Kind, resources.RunnableAgent)
	}
	if agent.Name != "go" {
		t.Fatalf("name = %q, want go", agent.Name)
	}
	if !agent.IsRunnable() {
		t.Fatal("agent is not recognized as a runnable agent")
	}

	registration, err := resources.AgentKindRegistrationFor(agent.Kind)
	if err != nil {
		t.Fatalf("registration for %q: %v", agent.Kind, err)
	}
	if got := registration.ExecutableName(agent.Name); got != "runnable-go" {
		t.Fatalf("executable = %q, want runnable-go", got)
	}
	if got := registration.GitHubRepository(agent.Name); got != "runnable-go" {
		t.Fatalf("repository = %q, want runnable-go", got)
	}
}
