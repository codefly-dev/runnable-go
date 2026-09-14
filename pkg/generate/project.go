package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"
	"github.com/codefly-dev/core/resources"
)

// Files of a generated Go runnable.
const (
	// ModuleFile declares the runnable's own Go module.
	ModuleFile = "go.mod"
	// BindingsFile carries the generated typed bindings.
	BindingsFile = "bindings.go"
)

// BuildInputs are the declared entrypoint inputs whose content changes the
// package beyond the handler itself.
//
// go.sum is deliberately absent: a runnable whose handler imports nothing
// outside the standard library has no go.sum, so requiring one would reject a
// valid declaration. What a build actually consumed is pinned by the build
// evidence, not by demanding the file exist.
func BuildInputs() []string {
	return []string{ModuleFile}
}

// PackageName is the Go package the generated bindings and the authored handler
// share. A runnable name is a path component, not a Go identifier, so it is
// reduced to one rather than interpolated as written.
func PackageName(name string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	reduced := b.String()
	if reduced == "" || !unicode.IsLetter(rune(reduced[0])) {
		return "", fmt.Errorf("runnable name %q does not reduce to a Go package name", name)
	}
	return reduced, nil
}

// HandlerFile is the resolved path of the declared entrypoint, checked to be a
// Go source file inside the runnable directory.
//
// The bindings the handler compiles against are written beside it, so a handler
// in a subdirectory would be in a different package from its own input and
// output types. That is rejected here rather than surfacing later as a compile
// error in generated code the author never wrote.
func HandlerFile(dir string, handler string) (string, error) {
	resolved, err := Confine(dir, handler)
	if err != nil {
		return "", err
	}
	if filepath.Ext(resolved) != ".go" {
		return "", fmt.Errorf("entrypoint handler %q is not a Go source file", handler)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if filepath.Dir(resolved) != root {
		return "", fmt.Errorf("entrypoint handler %q must sit in the runnable directory", handler)
	}
	return resolved, nil
}

// Scaffold writes the author-owned files of a runnable: the handler and the
// module declaration. Neither is rewritten if it already exists — the author
// owns both from their first write.
func Scaffold(runnable *resources.Runnable, dir string) error {
	pkg, err := PackageName(runnable.Name)
	if err != nil {
		return err
	}
	handler, err := HandlerFile(dir, runnable.Entrypoint.Handler)
	if err != nil {
		return err
	}
	if _, err := WriteHandler(handler, pkg, runnable.Name); err != nil {
		return err
	}
	module := filepath.Join(dir, ModuleFile)
	if _, err := os.Stat(module); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(module, moduleTemplate(pkg), 0o644)
}

// Generate writes the agent-owned files: the typed bindings, the invocation
// harness and the executable a launcher starts. Regenerating an unchanged
// declaration produces the same bytes.
func Generate(runnable *resources.Runnable, dir string) error {
	return GenerateForRelease(runnable, dir, nil)
}

// GenerateForRelease binds the harness to the workspace-resolved release, which
// it refuses an invocation naming anything else against. A nil identity is
// standalone generation, which has no owning workspace: the harness then checks
// the declaration's own name, module and version and leaves the workspace
// unchecked, because there is none to check against.
func GenerateForRelease(runnable *resources.Runnable, dir string, identity *basev0.RunnableIdentity) error {
	pkg, err := PackageName(runnable.Name)
	if err != nil {
		return err
	}
	if _, err := HandlerFile(dir, runnable.Entrypoint.Handler); err != nil {
		return err
	}
	bindings, err := Bindings(pkg, runnable.Contract)
	if err != nil {
		return err
	}
	harness, err := Harness(pkg, runnable, identity)
	if err != nil {
		return err
	}
	command, err := Command(pkg)
	if err != nil {
		return err
	}
	// Nothing is written until every file renders, so a declaration one of them
	// rejects does not leave a half-generated package behind.
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), bindings, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, HarnessFile), harness, 0o644); err != nil {
		return err
	}
	directory := filepath.Join(dir, CommandDirectory)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, CommandFile), command, 0o644)
}

// moduleTemplate declares the runnable's module at the toolchain the generated
// bindings require, so a build at an older one fails here rather than silently
// ignoring the omitzero option absence depends on.
func moduleTemplate(pkg string) []byte {
	return []byte(fmt.Sprintf("module %s\n\ngo %s\n", pkg, strings.TrimPrefix(GoRequirement, "go")))
}
