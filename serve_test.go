package main_test

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codefly-dev/core/agents"
	agentv0 "github.com/codefly-dev/core/generated/go/codefly/services/agent/v0"
	builderv0 "github.com/codefly-dev/core/generated/go/codefly/services/builder/v0"
	runtimev0 "github.com/codefly-dev/core/generated/go/codefly/services/runtime/v0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const agentToken = "serve-test-token"

// agentBinary is the compiled agent, built once for the package: linking it
// takes tens of seconds, and rebuilding per test both dominated the run time
// and made the agent's startup race the build.
var agentBinary string

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "runnable-go-serve")
	if err != nil {
		panic("creating the build directory: " + err.Error())
	}
	agentBinary = filepath.Join(directory, "runnable-go")
	build := exec.Command("go", "build", "-o", agentBinary, ".")
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		_ = os.RemoveAll(directory)
		panic("building the agent: " + buildErr.Error() + "\n" + string(output))
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

// served starts the real agent binary, completes the stdout handshake and
// returns a connection to it. Everything the CLI does to reach an agent is
// exercised here: nothing is stubbed.
func served(t *testing.T) (*grpc.ClientConn, *exec.Cmd, *bytes.Buffer) {
	t.Helper()

	cmd := exec.Command(agentBinary)
	// An empty UDS path selects the TCP loopback handshake, which avoids the
	// sun_path length limit on long temporary directory paths.
	cmd.Env = append(os.Environ(), "CODEFLY_AGENT_TOKEN="+agentToken, "CODEFLY_AGENT_UDS_PATH=")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the agent: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	endpoint := handshake(t, stdout, stderr)
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dialing %s: %v", endpoint, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, cmd, stderr
}

// handshake reads the "VERSION|endpoint" line the agent writes on startup. The
// version is checked rather than skipped: a process announcing an older
// protocol would be dialed with an endpoint spelling the CLI cannot use.
func handshake(t *testing.T, stdout interface{ Read([]byte) (int, error) }, stderr *bytes.Buffer) string {
	t.Helper()
	type read struct {
		line string
		err  error
	}
	lines := make(chan read, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		lines <- read{line: line, err: err}
	}()

	select {
	case result := <-lines:
		if result.err != nil {
			t.Fatalf("reading the handshake: %v\nagent stderr:\n%s", result.err, stderr.String())
		}
		version, endpoint, found := strings.Cut(strings.TrimSpace(result.line), "|")
		if !found {
			t.Fatalf("handshake %q is not VERSION|endpoint", result.line)
		}
		if version != strconv.Itoa(agents.ProtocolVersion) {
			t.Fatalf("handshake version = %s, want %d", version, agents.ProtocolVersion)
		}
		if endpoint == "" {
			t.Fatal("handshake carries no endpoint")
		}
		return endpoint
	case <-time.After(30 * time.Second):
		t.Fatalf("no handshake within 30s\nagent stderr:\n%s", stderr.String())
		return ""
	}
}

func authorized(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	return metadata.AppendToOutgoingContext(ctx, agents.AuthMetadataKey, agentToken), cancel
}

// The agent lifecycle served over gRPC is what issue #2 asks for first: a real
// process the CLI can reach, describing itself over the wire. Reaching the
// handler (rather than gRPC's Unimplemented) is what proves the Agent service
// was registered.
func TestAgentProcessServesItsInformation(t *testing.T) {
	conn, _, stderr := served(t)
	ctx, cancel := authorized(t)
	defer cancel()

	info, err := agentv0.NewAgentClient(conn).GetAgentInformation(ctx, &agentv0.AgentInformationRequest{})
	if err != nil {
		t.Fatalf("GetAgentInformation: %v\nagent stderr:\n%s", err, stderr.String())
	}
	languages := info.GetLanguages()
	if len(languages) != 1 || languages[0].GetType() != agentv0.Language_GO {
		t.Fatalf("languages = %v, want exactly GO", languages)
	}
	var builder, runtime bool
	for _, capability := range info.GetCapabilities() {
		switch capability.GetType() {
		case agentv0.Capability_BUILDER:
			builder = true
		case agentv0.Capability_RUNTIME, agentv0.Capability_HOT_RELOAD:
			runtime = true
		}
	}
	if !builder {
		t.Errorf("capabilities = %v, want BUILDER", info.GetCapabilities())
	}
	if runtime {
		t.Errorf("capabilities = %v, want no Runtime: the invocation process is supervised by the caller", info.GetCapabilities())
	}
}

// The advertised BUILDER must be reachable, not merely advertised: the CLI
// gates the phase on the capability and then dials.
//
// An empty request is the probe, because the answer distinguishes the two
// things a status code alone cannot. gRPC answers Unimplemented both for a
// service it has never heard of and for one falling through to its embedded
// UnimplementedBuilderServer; only a handler that actually ran can reject the
// request on its contents, which is what InvalidArgument here proves.
func TestAdvertisedBuilderIsReachableOnTheWire(t *testing.T) {
	conn, _, _ := served(t)
	ctx, cancel := authorized(t)
	defer cancel()

	_, err := builderv0.NewBuilderClient(conn).Load(ctx, &builderv0.LoadRequest{})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("Builder.Load code = %v, want InvalidArgument from the registered handler", got)
	}
	if message := status.Convert(err).Message(); !strings.Contains(message, "Runnable location is required") {
		t.Fatalf("Builder.Load said %q; want the loaded handler's own refusal", message)
	}
}

// The mirror invariant: a service that is neither advertised nor registered
// must be absent from the wire. This agent serves no Runtime, because a
// Runnable's invocation process is supervised by its caller.
//
// The message, not the code, is what shows absence — see above.
func TestUnservedRuntimeIsAbsentFromTheWire(t *testing.T) {
	conn, _, _ := served(t)
	ctx, cancel := authorized(t)
	defer cancel()

	_, err := runtimev0.NewRuntimeClient(conn).Load(ctx, &runtimev0.LoadRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("Runtime.Load code = %v, want Unimplemented for an unregistered service", got)
	}
	if message := status.Convert(err).Message(); !strings.Contains(message, "unknown service") {
		t.Fatalf("Runtime.Load said %q; want an unknown-service refusal, which means a Runtime is registered without being advertised", message)
	}
}

// The CLI's readiness probe is grpc.health.v1, not a port check. Without it the
// CLI races the agent between binding the listener and registering services.
func TestAgentProcessReportsHealthy(t *testing.T) {
	conn, _, stderr := served(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// The health service is deliberately exempt from auth: the probe runs
	// before the CLI has a token interceptor on the connection.
	resp, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check: %v\nagent stderr:\n%s", err, stderr.String())
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("health = %v, want SERVING", resp.GetStatus())
	}
}

// Anyone able to reach the loopback port must still present the per-spawn
// token. This is the only access control on the TCP path.
func TestUnauthenticatedCallIsRefused(t *testing.T) {
	conn, _, _ := served(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := agentv0.NewAgentClient(conn).GetAgentInformation(ctx, &agentv0.AgentInformationRequest{})
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("unauthenticated GetAgentInformation code = %v, want Unauthenticated", got)
	}
}

// The CLI sends SIGTERM and escalates to SIGKILL of the process group if the
// agent outlives the window, which orphans children. The process must exit on
// its own well inside that window.
func TestAgentProcessExitsOnSignal(t *testing.T) {
	_, cmd, stderr := served(t)

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("signalling the agent: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case <-exited:
	case <-time.After(20 * time.Second):
		t.Fatalf("agent did not exit within 20s of SIGINT\nagent stderr:\n%s", stderr.String())
	}
}
