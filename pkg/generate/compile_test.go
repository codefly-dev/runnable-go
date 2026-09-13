package generate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/runnable-go/pkg/generate"
)

// coveringContract exercises every value type of the bounded profile and every
// combination of optional and nullable.
func coveringContract() *resources.RunnableContract {
	return contractOf(
		[]*resources.RunnableField{
			field("text", resources.RunnableFieldString),
			{Name: "count", Type: resources.RunnableFieldInteger, Optional: true},
			{Name: "note", Type: resources.RunnableFieldString, Nullable: true},
			{Name: "flag", Type: resources.RunnableFieldBoolean, Optional: true, Nullable: true},
			{Name: "options", Type: resources.RunnableFieldObject, Optional: true, Fields: []*resources.RunnableField{
				field("limit", resources.RunnableFieldInteger),
			}},
			{Name: "scores", Type: resources.RunnableFieldArray, Items: field("", resources.RunnableFieldInteger)},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
}

// The generated bindings and scaffold are only correct if the Go toolchain and
// encoding/json agree with them, so they are compiled and exercised as the
// author workspace would compile them.
func TestGeneratedPackageCompilesAndKeepsContractSemantics(t *testing.T) {
	dir := t.TempDir()

	bindings, err := generate.Bindings("wordcount", coveringContract())
	if err != nil {
		t.Fatalf("Bindings: %v", err)
	}
	handler, err := generate.Handler("wordcount", "word-count")
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	for name, content := range map[string][]byte{
		"go.mod":            []byte("module wordcount\n\ngo 1.27.0\n"),
		"bindings.go":       bindings,
		"handler.go":        handler,
		"semantics_test.go": []byte(semanticsTest),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated package failed: %v\n%s\n--- bindings ---\n%s", err, out, bindings)
	}
}

const semanticsTest = `package wordcount

import (
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, payload string) Input {
	t.Helper()
	var in Input
	if err := json.Unmarshal([]byte(payload), &in); err != nil {
		t.Fatalf("decoding %s: %v", payload, err)
	}
	return in
}

func TestAbsentNullAndSetAreThreeStates(t *testing.T) {
	absent := decode(t, ` + "`" + `{"text":"a","note":null,"scores":[]}` + "`" + `)
	if absent.Flag.Present {
		t.Error("an absent optional-and-nullable key decoded as present")
	}
	if absent.Count != nil {
		t.Error("an absent optional key decoded as set")
	}
	if absent.Options != nil {
		t.Error("an absent optional object decoded as set")
	}
	if absent.Note != nil {
		t.Error("a null nullable key decoded as a value")
	}

	null := decode(t, ` + "`" + `{"text":"a","note":null,"flag":null,"scores":[]}` + "`" + `)
	if !null.Flag.Present || null.Flag.Value != nil {
		t.Errorf("a null optional-and-nullable key decoded as %+v, want present with no value", null.Flag)
	}

	set := decode(t, ` + "`" + `{"text":"a","note":"n","flag":true,"count":7,"options":{"limit":3},"scores":[1,2]}` + "`" + `)
	if !set.Flag.Present || set.Flag.Value == nil || !*set.Flag.Value {
		t.Errorf("a set optional-and-nullable key decoded as %+v", set.Flag)
	}
	if set.Count == nil || *set.Count != 7 {
		t.Errorf("count decoded as %v", set.Count)
	}
	if set.Note == nil || *set.Note != "n" {
		t.Errorf("note decoded as %v", set.Note)
	}
	if set.Options == nil || set.Options.Limit != 3 {
		t.Errorf("options decoded as %+v", set.Options)
	}
}

func TestEncodingOmitsAbsentKeysAndWritesNull(t *testing.T) {
	encoded := func(t *testing.T, in Input) string {
		t.Helper()
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("encoding: %v", err)
		}
		return string(data)
	}

	absent := encoded(t, Input{Text: "a", Scores: []int64{}})
	for _, key := range []string{` + "`" + `"flag"` + "`" + `, ` + "`" + `"count"` + "`" + `, ` + "`" + `"options"` + "`" + `} {
		if strings.Contains(absent, key) {
			t.Errorf("absent key %s was encoded: %s", key, absent)
		}
	}
	if !strings.Contains(absent, ` + "`" + `"note":null` + "`" + `) {
		t.Errorf("a nullable key was not encoded as null: %s", absent)
	}

	null := encoded(t, Input{Text: "a", Scores: []int64{}, Flag: Optional[bool]{Present: true}})
	if !strings.Contains(null, ` + "`" + `"flag":null` + "`" + `) {
		t.Errorf("a present-and-null key was not encoded as null: %s", null)
	}
}

func TestIntegerIsSigned64Bit(t *testing.T) {
	in := decode(t, ` + "`" + `{"text":"a","note":null,"scores":[9223372036854775807,-9223372036854775808]}` + "`" + `)
	if in.Scores[0] != 9223372036854775807 || in.Scores[1] != -9223372036854775808 {
		t.Errorf("64-bit bounds decoded as %v", in.Scores)
	}
	var overflow Input
	if err := json.Unmarshal([]byte(` + "`" + `{"text":"a","scores":[9223372036854775808]}` + "`" + `), &overflow); err == nil {
		t.Error("an integer past the 64-bit bound decoded without error")
	}
}

func TestNoImplicitCoercion(t *testing.T) {
	for _, payload := range []string{
		` + "`" + `{"text":"a","count":"7"}` + "`" + `,
		` + "`" + `{"text":"a","count":1.5}` + "`" + `,
		` + "`" + `{"text":5}` + "`" + `,
		` + "`" + `{"text":"a","flag":"true"}` + "`" + `,
		` + "`" + `{"text":"a","scores":[true]}` + "`" + `,
		` + "`" + `{"text":"a","options":[]}` + "`" + `,
	} {
		var in Input
		if err := json.Unmarshal([]byte(payload), &in); err == nil {
			t.Errorf("%s decoded without error", payload)
		}
	}
}
`
