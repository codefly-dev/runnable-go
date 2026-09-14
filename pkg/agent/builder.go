package agent

import (
	"context"
	"path/filepath"
	"slices"
	"sync"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"
	builderv0 "github.com/codefly-dev/core/generated/go/codefly/services/builder/v0"
	"github.com/codefly-dev/core/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/codefly-dev/runnable-go/pkg/generate"
)

// Builder owns one loaded declaration.
//
// That state is per-process, not per-caller: the lock serializes calls but
// cannot tell two callers apart, and Load replaces whatever it finds. One
// connection therefore drives one Runnable at a time. A caller building several
// concurrently needs one agent process each, which is what manager.Load gives
// it.
type Builder struct {
	builderv0.UnimplementedBuilderServer
	mu       sync.Mutex
	agent    *resources.Agent
	runnable *resources.Runnable
	identity *basev0.RunnableIdentity
}

// NewBuilder returns the Builder bound to the agent's own manifest, which Load
// checks a declaration's pinned agent against.
func NewBuilder(agent *resources.Agent) *Builder {
	return &Builder{agent: agent}
}

// Load resolves the runnable a RUNNABLE-kind agent was started for.
//
// Nothing here trusts the request's spelling on its own. The location names a
// release and a directory, and both are re-derived from the workspace
// declaration and compared: a request naming one release but pointing at
// another runnable's directory would otherwise have this agent generate into,
// and later package, a tree its caller never named.
func (b *Builder) Load(ctx context.Context, request *builderv0.LoadRequest) (*builderv0.LoadResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.runnable, b.identity = nil, nil

	if request.GetIdentity() != nil || request.GetRunnable() == nil {
		return nil, status.Error(codes.InvalidArgument, "a Runnable location is required; service identity is unsupported")
	}
	location := request.GetRunnable()
	if err := resources.Validate(location); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid Runnable location: %v", err)
	}
	workspace, err := resources.LoadWorkspaceFromDir(ctx, location.GetWorkspacePath())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "load workspace: %v", err)
	}
	id := location.GetIdentity()
	runnable, err := workspace.LoadRunnableFromUnique(ctx, id.GetModule()+"/"+id.GetName())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "load Runnable: %v", err)
	}
	actual, err := workspace.RunnableLocationOf(runnable)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "locate the Runnable in its workspace: %v", err)
	}
	actualWire, err := actual.Proto()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode the Runnable location: %v", err)
	}
	if !proto.Equal(actualWire.GetIdentity(), id) {
		return nil, status.Error(codes.InvalidArgument, "Runnable release identity does not match the workspace declaration")
	}
	// Compared in resolved form: two spellings of one directory are the same
	// tree, and a symlinked one is whatever it points at.
	declaredDir, err := filepath.EvalSymlinks(runnable.Dir())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve the declared Runnable directory: %v", err)
	}
	requestedDir, err := filepath.EvalSymlinks(resources.RunnableLocationFromProto(location).Dir())
	if err != nil || declaredDir != requestedDir {
		return nil, status.Error(codes.InvalidArgument, "Runnable location does not match the workspace declaration")
	}
	if runnable.Agent == nil || !runnable.Agent.IsRunnable() || runnable.Agent.Identifier() != b.agent.Identifier() {
		return nil, status.Error(codes.InvalidArgument, "declaration pins a different Runnable agent")
	}
	if _, err := generate.HandlerFile(runnable.Dir(), runnable.Entrypoint.Handler); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	b.runnable, b.identity = runnable, proto.Clone(id).(*basev0.RunnableIdentity)
	return &builderv0.LoadResponse{State: &builderv0.LoadStatus{State: builderv0.LoadStatus_READY}}, nil
}

// Create scaffolds the author's files and generates the typed bindings and the
// invocation harness.
//
// The handler and the module declaration are written only when absent, so
// creating twice cannot replace an implementation with a scaffold that returns
// "handler is not implemented". The bindings and the harness are agent-owned
// and rewritten, bound to the release Load resolved.
func (b *Builder) Create(ctx context.Context, _ *builderv0.CreateRequest) (*builderv0.CreateResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.runnable == nil {
		return nil, status.Error(codes.FailedPrecondition, "load a Runnable before creation")
	}
	runnable := b.runnable
	if err := generate.Scaffold(runnable, runnable.Dir()); err != nil {
		return nil, status.Errorf(codes.Internal, "scaffold the Runnable: %v", err)
	}
	// The declaration must name what a rebuild depends on, or build evidence
	// would pin a handler while the module that resolves its imports changed
	// underneath it unrecorded.
	changed := false
	for _, input := range generate.BuildInputs() {
		if !slices.Contains(runnable.Entrypoint.Inputs, input) {
			runnable.Entrypoint.Inputs = append(runnable.Entrypoint.Inputs, input)
			changed = true
		}
	}
	if changed {
		if err := runnable.Save(ctx); err != nil {
			return nil, status.Errorf(codes.Internal, "save the declaration: %v", err)
		}
	}
	if err := generate.GenerateForRelease(runnable, runnable.Dir(), b.identity); err != nil {
		return nil, status.Errorf(codes.Internal, "generate the bindings: %v", err)
	}
	return &builderv0.CreateResponse{State: &builderv0.CreateStatus{State: builderv0.CreateStatus_CREATED}}, nil
}

// RunnableBuildInputs reports the build facts only this agent holds.
//
// UNSUPPORTED is the contract's own word for a phase an agent does not
// implement, and it is returned in band rather than as a gRPC Unimplemented so
// a caller reads one refusal shape from the whole Builder. The harness the
// digest would measure is generated now, but a RunnableBuild also pins the
// toolchain and the harness as they were archived, and this agent prepares no
// tree to measure that from.
func (b *Builder) RunnableBuildInputs(_ context.Context, _ *builderv0.RunnableBuildInputsRequest) (*builderv0.RunnableBuildInputsResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.runnable == nil {
		return nil, status.Error(codes.FailedPrecondition, "load a Runnable before building")
	}
	return &builderv0.RunnableBuildInputsResponse{
		State: &builderv0.RunnableBuildInputsStatus{
			State:   builderv0.RunnableBuildInputsStatus_UNSUPPORTED,
			Message: "native Go packaging is not implemented, so there is no prepared tree to measure build inputs from",
		},
	}, nil
}

// Package emits the native artifact. Unsupported for the same reason as
// RunnableBuildInputs: this agent generates the harness but does not compile or
// archive it, so there is no artifact to name and no digest to report.
func (b *Builder) Package(_ context.Context, _ *builderv0.PackageRequest) (*builderv0.PackageResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return &builderv0.PackageResponse{
		State: &builderv0.PackageStatus{
			State:   builderv0.PackageStatus_UNSUPPORTED,
			Message: "native Go packaging is not implemented: the generated harness is neither compiled nor archived yet",
		},
	}, nil
}
