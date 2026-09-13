package generate

import (
	"fmt"
	"go/format"
	"strings"
)

// Handler renders the author entrypoint of a runnable. The scaffold is written
// once and owned by the author from then on, so it carries no generated marker
// and is never rewritten over their implementation.
func Handler(pkg string, name string) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import (\n\t\"context\"\n\t\"errors\"\n)\n\n")
	fmt.Fprintf(&b, "// Handle runs one invocation of the %s runnable.\n", name)
	fmt.Fprintf(&b, "func Handle(ctx context.Context, in %s) (%s, error) {\n", inputType, outputType)
	fmt.Fprintf(&b, "\treturn %s{}, errors.New(\"handler is not implemented\")\n", outputType)
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}
