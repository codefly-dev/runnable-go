package generate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/runnable-go/pkg/generate"
)

// coveringContract exercises every value type of the bounded profile and every
// combination of optional and nullable, on a scalar and on an array both.
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
			{Name: "tags", Type: resources.RunnableFieldArray, Optional: true, Items: field("", resources.RunnableFieldString)},
			{Name: "marks", Type: resources.RunnableFieldArray, Nullable: true, Items: field("", resources.RunnableFieldInteger)},
		},
		[]*resources.RunnableField{
			field("total", resources.RunnableFieldInteger),
			{Name: "results", Type: resources.RunnableFieldArray, Items: field("", resources.RunnableFieldString)},
		},
	)
}

// goDirective is the language version the generated module is compiled at. It
// is read from this repository's go.mod rather than repeated here, so the
// compile test cannot go on exercising a version the repository has left.
func goDirective(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod declares no go directive")
	return ""
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
		"go.mod":            []byte("module wordcount\n\ngo " + goDirective(t) + "\n"),
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

func encoded(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return string(data)
}

func TestAbsentNullAndSetAreThreeStates(t *testing.T) {
	absent := decode(t, ` + "`" + `{"text":"a","note":null,"scores":[],"marks":null}` + "`" + `)
	if absent.Flag.Present() {
		t.Error("an absent optional-and-nullable key decoded as present")
	}
	if _, ok := absent.Count.Get(); ok {
		t.Error("an absent optional key decoded as set")
	}
	if _, ok := absent.Options.Get(); ok {
		t.Error("an absent optional object decoded as set")
	}
	if absent.Note != nil {
		t.Error("a null nullable key decoded as a value")
	}

	null := decode(t, ` + "`" + `{"text":"a","note":null,"flag":null,"scores":[],"marks":null}` + "`" + `)
	if _, ok := null.Flag.Get(); !null.Flag.Present() || ok {
		t.Errorf("a null optional-and-nullable key decoded as %+v, want present with no value", null.Flag)
	}

	set := decode(t, ` + "`" + `{"text":"a","note":"n","flag":true,"count":7,"options":{"limit":3},"scores":[1,2],"marks":[3]}` + "`" + `)
	flag, ok := set.Flag.Get()
	if !set.Flag.Present() || !ok || !flag {
		t.Errorf("a set optional-and-nullable key decoded as %+v", set.Flag)
	}
	if count, ok := set.Count.Get(); !ok || count != 7 {
		t.Errorf("count decoded as %+v", set.Count)
	}
	if set.Note == nil || *set.Note != "n" {
		t.Errorf("note decoded as %v", set.Note)
	}
	if options, ok := set.Options.Get(); !ok || options.Limit != 3 {
		t.Errorf("options decoded as %+v", set.Options)
	}
	if set.Marks == nil || len(*set.Marks) != 1 {
		t.Errorf("marks decoded as %v", set.Marks)
	}
}

func TestEncodingOmitsAbsentKeysAndWritesNull(t *testing.T) {
	absent := encoded(t, Input{Text: "a", Scores: List[int64]{}})
	for _, key := range []string{` + "`" + `"flag"` + "`" + `, ` + "`" + `"count"` + "`" + `, ` + "`" + `"options"` + "`" + `, ` + "`" + `"tags"` + "`" + `} {
		if strings.Contains(absent, key) {
			t.Errorf("absent key %s was encoded: %s", key, absent)
		}
	}
	for _, key := range []string{` + "`" + `"note":null` + "`" + `, ` + "`" + `"marks":null` + "`" + `} {
		if !strings.Contains(absent, key) {
			t.Errorf("a nullable key was not encoded as %s: %s", key, absent)
		}
	}

	null := encoded(t, Input{Text: "a", Scores: List[int64]{}, Flag: Null[bool]()})
	if !strings.Contains(null, ` + "`" + `"flag":null` + "`" + `) {
		t.Errorf("a present-and-null key was not encoded as null: %s", null)
	}

	set := encoded(t, Input{Text: "a", Scores: List[int64]{}, Count: Value(int64(7))})
	if !strings.Contains(set, ` + "`" + `"count":7` + "`" + `) {
		t.Errorf("a set optional key was not encoded: %s", set)
	}
}

// A field the contract declares optional and not nullable has no null state, so
// a null is refused rather than decoded as the absent key it is not.
func TestNullIsRefusedForAnOptionalFieldThatIsNotNullable(t *testing.T) {
	for _, payload := range []string{
		` + "`" + `{"text":"a","note":null,"scores":[],"count":null}` + "`" + `,
		` + "`" + `{"text":"a","note":null,"scores":[],"options":null}` + "`" + `,
	} {
		var in Input
		if err := json.Unmarshal([]byte(payload), &in); err == nil {
			t.Errorf("%s decoded without error", payload)
		}
	}

	var in Input
	if err := json.Unmarshal([]byte(` + "`" + `{"text":"a","note":null,"flag":null,"scores":[]}` + "`" + `), &in); err != nil {
		t.Errorf("a null for a key the contract declares nullable was refused: %v", err)
	}
}

// An array the contract does not declare nullable has no null state, so an
// unset one is the empty array it is. The scaffold returns a zero Output, so
// this is the very first payload a generated runnable produces.
func TestAnUnsetArrayEncodesAsAnEmptyArrayAndNeverAsNull(t *testing.T) {
	zero := encoded(t, Output{})
	if !strings.Contains(zero, ` + "`" + `"results":[]` + "`" + `) {
		t.Errorf("a zero Output encoded its required array as %s", zero)
	}
	if strings.Contains(zero, "null") {
		t.Errorf("a zero Output put a null on the wire: %s", zero)
	}

	roundTripped := encoded(t, decode(t, ` + "`" + `{"text":"a","note":null,"scores":[1],"marks":null}` + "`" + `))
	if strings.Contains(roundTripped, ` + "`" + `"scores":null` + "`" + `) {
		t.Errorf("decoding and re-encoding turned an array into null: %s", roundTripped)
	}
	for _, key := range []string{` + "`" + `"count"` + "`" + `, ` + "`" + `"flag"` + "`" + `, ` + "`" + `"tags"` + "`" + `} {
		if strings.Contains(roundTripped, key) {
			t.Errorf("decoding and re-encoding turned absent key %s into a written one: %s", key, roundTripped)
		}
	}
}

// A null for an array the contract does not declare nullable is refused rather
// than decoded as an empty or absent array.
func TestNullIsRefusedForAnArrayThatIsNotNullable(t *testing.T) {
	for _, payload := range []string{
		` + "`" + `{"text":"a","scores":null}` + "`" + `,
		` + "`" + `{"text":"a","scores":[],"tags":null}` + "`" + `,
	} {
		var in Input
		if err := json.Unmarshal([]byte(payload), &in); err == nil {
			t.Errorf("%s decoded without error", payload)
		}
	}
	var in Input
	if err := json.Unmarshal([]byte(` + "`" + `{"text":"a","scores":[],"marks":null}` + "`" + `), &in); err != nil {
		t.Errorf("a null for a nullable array was refused: %v", err)
	} else if in.Marks != nil {
		t.Errorf("a null nullable array decoded as %v", in.Marks)
	}
}

// A value handed to an optional-and-nullable field is on the wire. It cannot
// be set without also being present, and an absent one is never written as a
// null that would claim the author chose null.
func TestASetValueIsNeverDroppedAndAnAbsentOneIsNeverANull(t *testing.T) {
	written := encoded(t, Input{Text: "a", Scores: List[int64]{}, Flag: Set(true)})
	if !strings.Contains(written, ` + "`" + `"flag":true` + "`" + `) {
		t.Errorf("a set value was dropped from the payload: %s", written)
	}

	if _, err := json.Marshal(Absent[bool]()); err == nil {
		t.Error("an absent optional encoded as a value instead of being refused")
	}
	if value, ok := Absent[bool]().Get(); ok || value {
		t.Errorf("an absent optional reported a value: %v %v", value, ok)
	}
}

func TestIntegerIsSigned64Bit(t *testing.T) {
	in := decode(t, ` + "`" + `{"text":"a","note":null,"scores":[9223372036854775807,-9223372036854775808],"marks":null}` + "`" + `)
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
		` + "`" + `{"text":"a","scores":{}}` + "`" + `,
	} {
		var in Input
		if err := json.Unmarshal([]byte(payload), &in); err == nil {
			t.Errorf("%s decoded without error", payload)
		}
	}
}
`
