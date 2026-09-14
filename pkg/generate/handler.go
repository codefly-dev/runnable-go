package generate

import (
	"fmt"
	"go/format"
	"os"
	"strings"
	"unicode"
)

// Handler renders the author entrypoint of a runnable. The scaffold carries no
// generated marker because the author owns it from its first write onward.
//
// Handler renders; it does not write. WriteHandler is what keeps the scaffold
// from being written over an implementation, and is the only way this package
// puts a handler on disk.
func Handler(pkg string, name string) ([]byte, error) {
	if err := validatePackageName(pkg); err != nil {
		return nil, err
	}
	if err := validateRunnableName(name); err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import (\n\t\"context\"\n\t\"errors\"\n)\n\n")
	fmt.Fprintf(&b, "// Handle runs one invocation of the %s runnable.\n", name)
	fmt.Fprintf(&b, "func Handle(ctx context.Context, in %s) (%s, error) {\n", inputType, outputType)
	fmt.Fprintf(&b, "\treturn %s{}, errors.New(\"handler is not implemented\")\n", outputType)
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

// WriteHandler writes the scaffold at path unless a file is already there, and
// reports whether it wrote one.
//
// The author owns the handler from its first write, so regenerating a runnable
// must never rewrite it: an implementation overwritten by a scaffold that
// returns "handler is not implemented" is gone. The file is created
// exclusively rather than checked and then written, so two generations racing
// on one path cannot both decide the file is absent.
func WriteHandler(path string, pkg string, name string) (bool, error) {
	content, err := Handler(pkg, name)
	if err != nil {
		return false, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return false, err
	}
	return true, file.Close()
}

// validateRunnableName rejects a name that would not stay inside the comment it
// is rendered into. A runnable name is a path component, not a Go identifier,
// so it is quoted into prose rather than into code — but a line terminator in
// it would end the comment and put the rest of the name in the file as source.
func validateRunnableName(name string) error {
	if name == "" {
		return fmt.Errorf("runnable name is empty")
	}
	for _, r := range name {
		if r == '\n' || r == '\r' || unicode.IsControl(r) {
			return fmt.Errorf("runnable name %q carries a control character", name)
		}
	}
	return nil
}
