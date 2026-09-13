package generate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/runnable-go/pkg/generate"
)

func field(name string, t resources.RunnableFieldType) *resources.RunnableField {
	return &resources.RunnableField{Name: name, Type: t}
}

func contractOf(input, output []*resources.RunnableField) *resources.RunnableContract {
	return &resources.RunnableContract{
		Protocol: resources.RunnableProtocolV1,
		Input:    &resources.RunnableSchema{Fields: input},
		Output:   &resources.RunnableSchema{Fields: output},
	}
}

func generated(t *testing.T, input, output []*resources.RunnableField) string {
	t.Helper()
	source, err := generate.Bindings("wordcount", contractOf(input, output))
	if err != nil {
		t.Fatalf("Bindings: %v", err)
	}
	return string(source)
}

// requireLine matches a declaration regardless of the column padding gofmt
// aligns struct fields with.
func requireLine(t *testing.T, source string, want string) {
	t.Helper()
	for _, line := range strings.Split(source, "\n") {
		if strings.Join(strings.Fields(line), " ") == want {
			return
		}
	}
	t.Fatalf("generated source has no line %q:\n%s", want, source)
}

// Each value type of the bounded profile has exactly one Go spelling, and an
// integer is the signed 64-bit one the contract declares.
func TestValueTypesMapToTheBoundedProfile(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{
			field("text", resources.RunnableFieldString),
			field("count", resources.RunnableFieldInteger),
			field("enabled", resources.RunnableFieldBoolean),
			{Name: "tags", Type: resources.RunnableFieldArray, Items: field("", resources.RunnableFieldString)},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)

	requireLine(t, source, "Text string `json:\"text\"`")
	requireLine(t, source, "Count int64 `json:\"count\"`")
	requireLine(t, source, "Enabled bool `json:\"enabled\"`")
	requireLine(t, source, "Tags List[string] `json:\"tags\"`")
	requireLine(t, source, "Total int64 `json:\"total\"`")
}

// Optional and nullable are declared independently, so the four combinations
// are four distinct Go spellings.
func TestOptionalAndNullableAreIndependent(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{
			field("required", resources.RunnableFieldString),
			{Name: "absent_or_set", Type: resources.RunnableFieldString, Optional: true},
			{Name: "null_or_set", Type: resources.RunnableFieldString, Nullable: true},
			{Name: "absent_or_null_or_set", Type: resources.RunnableFieldString, Optional: true, Nullable: true},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)

	requireLine(t, source, "Required string `json:\"required\"`")
	requireLine(t, source, "AbsentOrSet Present[string] `json:\"absent_or_set,omitzero\"`")
	requireLine(t, source, "NullOrSet *string `json:\"null_or_set\"`")
	requireLine(t, source, "AbsentOrNullOrSet Optional[string] `json:\"absent_or_null_or_set,omitzero\"`")
	if !strings.Contains(source, "type Optional[T any] struct") {
		t.Fatalf("Optional is used but not declared:\n%s", source)
	}
	if !strings.Contains(source, "type Present[T any] struct") {
		t.Fatalf("Present is used but not declared:\n%s", source)
	}
}

// An optional, non-nullable field is a Present and never a pointer:
// encoding/json sets a pointer field to nil for an explicit null without
// consulting an unmarshaler, which would admit a null the declaration forbids
// and leave it indistinguishable from the key being absent.
func TestOptionalAndNotNullableIsNeverAPointer(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{
			{Name: "label", Type: resources.RunnableFieldString, Optional: true},
			{Name: "count", Type: resources.RunnableFieldInteger, Optional: true},
			{Name: "enabled", Type: resources.RunnableFieldBoolean, Optional: true},
			{Name: "options", Type: resources.RunnableFieldObject, Optional: true, Fields: []*resources.RunnableField{
				field("limit", resources.RunnableFieldInteger),
			}},
			{Name: "tags", Type: resources.RunnableFieldArray, Optional: true, Items: field("", resources.RunnableFieldString)},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)

	requireLine(t, source, "Label Present[string] `json:\"label,omitzero\"`")
	requireLine(t, source, "Count Present[int64] `json:\"count,omitzero\"`")
	requireLine(t, source, "Enabled Present[bool] `json:\"enabled,omitzero\"`")
	requireLine(t, source, "Options Present[InputOptions] `json:\"options,omitzero\"`")
	requireLine(t, source, "Tags Present[List[string]] `json:\"tags,omitzero\"`")
}

// A carrier is only declared when the contract needs it.
func TestCarriersAreNotDeclaredWhenUnused(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{{Name: "note", Type: resources.RunnableFieldString, Nullable: true}},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	for _, carrier := range []string{"Optional", "Present"} {
		if strings.Contains(source, carrier) {
			t.Fatalf("%s declared for a contract that has no optional field:\n%s", carrier, source)
		}
	}
	if strings.Contains(source, "encoding/json") {
		t.Fatalf("bindings import encoding/json without a carrier:\n%s", source)
	}
}

// Nested objects are named after the path that reaches them, so two objects
// with the same field name in different parents stay distinct types.
func TestNestedObjectsAreNamedByPath(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{
			{Name: "options", Type: resources.RunnableFieldObject, Optional: true, Fields: []*resources.RunnableField{
				{Name: "stop_words", Type: resources.RunnableFieldArray, Nullable: true, Items: field("", resources.RunnableFieldString)},
			}},
			{Name: "records", Type: resources.RunnableFieldArray, Items: &resources.RunnableField{
				Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{field("id", resources.RunnableFieldString)},
			}},
		},
		[]*resources.RunnableField{
			{Name: "options", Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{
				field("stop_words", resources.RunnableFieldString),
			}},
		},
	)

	requireLine(t, source, "Options Present[InputOptions] `json:\"options,omitzero\"`")
	requireLine(t, source, "StopWords *List[string] `json:\"stop_words\"`")
	requireLine(t, source, "Records List[InputRecordsItem] `json:\"records\"`")
	requireLine(t, source, "type InputRecordsItem struct {")
	requireLine(t, source, "type OutputOptions struct {")
}

// A nullable array element is a pointer; the contract forbids an optional one.
func TestNullableArrayElements(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{
			{Name: "scores", Type: resources.RunnableFieldArray, Items: &resources.RunnableField{Type: resources.RunnableFieldInteger, Nullable: true}},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	requireLine(t, source, "Scores List[*int64] `json:\"scores\"`")
}

// A field description reaches the author on the generated field.
func TestDescriptionsAreCarriedToFields(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{{Name: "text", Type: resources.RunnableFieldString, Description: "the document to count"}},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	requireLine(t, source, "// the document to count")
}

// Two wire names that are distinct to the contract but identical in Go are a
// generation failure, not a silently dropped field.
func TestCollidingGoFieldNamesAreRejected(t *testing.T) {
	_, err := generate.Bindings("wordcount", contractOf(
		[]*resources.RunnableField{field("stop_words", resources.RunnableFieldString), field("stopWords", resources.RunnableFieldString)},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	))
	if err == nil {
		t.Fatal("colliding Go field names generated without error")
	}
	if !strings.Contains(err.Error(), "StopWords") {
		t.Fatalf("error does not name the colliding Go field: %v", err)
	}
}

func TestFieldNameWithoutGoIdentifierIsRejected(t *testing.T) {
	_, err := generate.Bindings("wordcount", contractOf(
		[]*resources.RunnableField{field("_", resources.RunnableFieldString)},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	))
	if err == nil {
		t.Fatal("field name with no Go identifier generated without error")
	}
}

// Generation runs core's contract rules rather than its own, so a declaration
// core rejects never reaches a binding file.
func TestContractIsValidatedByCore(t *testing.T) {
	cases := map[string]*resources.RunnableContract{
		"unsupported protocol": {
			Protocol: "codefly.runnable/v2",
			Input:    &resources.RunnableSchema{Fields: []*resources.RunnableField{field("text", resources.RunnableFieldString)}},
			Output:   &resources.RunnableSchema{Fields: []*resources.RunnableField{field("total", resources.RunnableFieldInteger)}},
		},
		"type outside the profile": contractOf(
			[]*resources.RunnableField{field("amount", resources.RunnableFieldType("number"))},
			[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
		),
		"array without items": contractOf(
			[]*resources.RunnableField{{Name: "tags", Type: resources.RunnableFieldArray}},
			[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
		),
		"optional array element": contractOf(
			[]*resources.RunnableField{{Name: "tags", Type: resources.RunnableFieldArray, Items: &resources.RunnableField{Type: resources.RunnableFieldString, Optional: true}}},
			[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
		),
		"field declared twice": contractOf(
			[]*resources.RunnableField{field("text", resources.RunnableFieldString), field("text", resources.RunnableFieldString)},
			[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
		),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := generate.Bindings("wordcount", contract); err == nil {
				t.Fatal("invalid contract generated without error")
			}
		})
	}
}

func TestHandlerScaffold(t *testing.T) {
	source, err := generate.Handler("wordcount", "word-count")
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	text := string(source)
	requireLine(t, text, "package wordcount")
	requireLine(t, text, "func Handle(ctx context.Context, in Input) (Output, error) {")
	if !strings.Contains(text, "word-count") {
		t.Fatalf("scaffold does not name the runnable:\n%s", text)
	}
	if strings.Contains(text, "DO NOT EDIT") {
		t.Fatalf("author-owned scaffold is marked generated:\n%s", text)
	}
}

// A contract field name that is legal to core but generates no exported Go
// identifier is named in the error, rather than reaching gofmt as a parse
// error pointing at a file the author never sees.
func TestFieldNameWithoutExportedIdentifierIsRejected(t *testing.T) {
	for _, name := range []string{"_1", "_1x"} {
		t.Run(name, func(t *testing.T) {
			contract := contractOf(
				[]*resources.RunnableField{field(name, resources.RunnableFieldString)},
				[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
			)
			if err := contract.Validate(); err != nil {
				t.Fatalf("core rejects %q, so this is no longer the case under test: %v", name, err)
			}
			_, err := generate.Bindings("wordcount", contract)
			if err == nil {
				t.Fatal("field name with no exported Go identifier generated without error")
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error does not name the offending contract field: %v", err)
			}
			if strings.Contains(err.Error(), "expected") {
				t.Fatalf("error is a gofmt parse error rather than a diagnosis: %v", err)
			}
		})
	}
}

// Two contract paths that generate one Go type name are reported as the two
// paths an author can act on. The generated name is not a name they wrote.
func TestCollidingTypeNamesNameBothContractPaths(t *testing.T) {
	contract := contractOf(
		[]*resources.RunnableField{
			{Name: "a_b", Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{field("x", resources.RunnableFieldString)}},
			{Name: "a", Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{
				{Name: "b", Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{field("y", resources.RunnableFieldString)}},
			}},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	if err := contract.Validate(); err != nil {
		t.Fatalf("core rejects the contract, so this is no longer the case under test: %v", err)
	}
	_, err := generate.Bindings("wordcount", contract)
	if err == nil {
		t.Fatal("colliding Go type names generated without error")
	}
	for _, path := range []string{"input.a_b", "input.a.b"} {
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("error does not name contract path %s: %v", path, err)
		}
	}
}

// A runnable name is a path component, so it may carry characters no Go
// package name may. Rendering one unvalidated puts it in the file as source.
func TestPackageNameMustBeAGoIdentifier(t *testing.T) {
	contract := contractOf(
		[]*resources.RunnableField{field("text", resources.RunnableFieldString)},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	for _, pkg := range []string{"word-count", "", "go", "_", "1count", "wordcount\n\nfunc init() {}"} {
		t.Run(pkg, func(t *testing.T) {
			if _, err := generate.Bindings(pkg, contract); err == nil {
				t.Fatalf("package name %q rendered without error", pkg)
			} else if !strings.Contains(err.Error(), "package name") {
				t.Fatalf("error does not diagnose the package name: %v", err)
			}
			if _, err := generate.Handler(pkg, "word-count"); err == nil {
				t.Fatalf("Handler rendered package name %q without error", pkg)
			}
		})
	}
}

// The runnable name is rendered into a comment, so a line terminator in it
// would end the comment and leave the rest of the name in the file as source.
func TestRunnableNameMustStayInsideItsComment(t *testing.T) {
	for _, name := range []string{"", "word\ncount", "word\rcount", "word\x00count"} {
		if _, err := generate.Handler("wordcount", name); err == nil {
			t.Fatalf("runnable name %q rendered without error", name)
		}
	}
}

// Generated bindings only encode an absent key correctly under a toolchain
// that honours omitzero, so they say so in the file rather than leaving it to
// whatever compiles the author's workspace.
func TestGeneratedFileCarriesTheToolchainConstraint(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{{Name: "count", Type: resources.RunnableFieldInteger, Optional: true}},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	want := "//go:build " + generate.GoRequirement
	if !strings.HasPrefix(source, want+"\n") {
		t.Fatalf("generated file does not open with %q:\n%s", want, source)
	}
}

// The Optional states are reachable only through the constructors: no field of
// it is exported, so no literal can spell a state the contract does not have.
func TestOptionalStatesAreOnlyReachableThroughConstructors(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{{Name: "flag", Type: resources.RunnableFieldBoolean, Optional: true, Nullable: true}},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	for _, exported := range []string{"Present bool", "Value *T"} {
		if strings.Contains(source, exported) {
			t.Fatalf("Optional exports %q, so a literal can set a value without marking it present:\n%s", exported, source)
		}
	}
	for _, constructor := range []string{"func Absent[T any]() Optional[T]", "func Null[T any]() Optional[T]", "func Set[T any](value T) Optional[T]"} {
		if !strings.Contains(source, constructor) {
			t.Fatalf("Optional has no %q:\n%s", constructor, source)
		}
	}
}

// An array is a List, and an optional one is carried by Present rather than by
// a pointer: both refuse the null this field does not declare, where a pointer
// would have decoded it as the absent key it is not.
func TestOptionalArrayIsCarriedAndNeverAPointer(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{
			{Name: "tags", Type: resources.RunnableFieldArray, Optional: true, Items: field("", resources.RunnableFieldString)},
			{Name: "marks", Type: resources.RunnableFieldArray, Optional: true, Nullable: true, Items: field("", resources.RunnableFieldString)},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	requireLine(t, source, "Tags Present[List[string]] `json:\"tags,omitzero\"`")
	requireLine(t, source, "Marks Optional[List[string]] `json:\"marks,omitzero\"`")
}

// The rule that turns a contract name into a generated type name is shared
// with the other runnable agents: an author reading the Go and the Python
// bindings of one contract must see the same names. It is duplicated in
// runnable-python pkg/generate/types.go (camel and nestedName) until it moves
// into core, so it is pinned here against the names that implementation emits.
func TestNamingIsTheSharedCrossLanguageRule(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{
			{Name: "stop_words", Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{
				{Name: "by_language", Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{field("id", resources.RunnableFieldString)}},
			}},
			{Name: "records", Type: resources.RunnableFieldArray, Items: &resources.RunnableField{
				Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{
					{Name: "sub_records", Type: resources.RunnableFieldArray, Items: &resources.RunnableField{
						Type: resources.RunnableFieldObject, Fields: []*resources.RunnableField{field("id", resources.RunnableFieldString)},
					}},
				},
			}},
		},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	for _, name := range []string{
		"type InputStopWords struct {",
		"type InputStopWordsByLanguage struct {",
		"type InputRecordsItem struct {",
		"type InputRecordsItemSubRecordsItem struct {",
	} {
		requireLine(t, source, name)
	}
}

// Generated code is read as a diff by the author who owns the workspace it
// lands in, so the whole file is pinned: declaration order, the header, the
// blank lines and the single emission of each carrier.
func TestGeneratedFileMatchesItsGolden(t *testing.T) {
	source, err := generate.Bindings("wordcount", coveringContract())
	if err != nil {
		t.Fatalf("Bindings: %v", err)
	}
	golden := filepath.Join("testdata", "covering_bindings.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, source, 0o600); err != nil {
			t.Fatalf("writing %s: %v", golden, err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading %s: %v", golden, err)
	}
	if string(source) != string(want) {
		t.Fatalf("generated bindings differ from %s; re-run with UPDATE_GOLDEN=1 to accept\n--- got ---\n%s", golden, source)
	}
}

// The scaffold is the author's from its first write, so a second generation
// leaves an implementation exactly as it found it.
func TestWriteHandlerNeverOverwritesAnImplementation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handler.go")

	written, err := generate.WriteHandler(path, "wordcount", "word-count")
	if err != nil {
		t.Fatalf("WriteHandler: %v", err)
	}
	if !written {
		t.Fatal("the first WriteHandler wrote nothing")
	}

	implementation := []byte("package wordcount\n\n// the author's work\n")
	if err := os.WriteFile(path, implementation, 0o600); err != nil {
		t.Fatalf("writing the implementation: %v", err)
	}

	written, err = generate.WriteHandler(path, "wordcount", "word-count")
	if err != nil {
		t.Fatalf("second WriteHandler: %v", err)
	}
	if written {
		t.Fatal("WriteHandler reported writing over an existing handler")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(got) != string(implementation) {
		t.Fatalf("the author's handler was rewritten:\n%s", got)
	}
}
