package generate_test

import (
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
	requireLine(t, source, "Tags []string `json:\"tags\"`")
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
	requireLine(t, source, "AbsentOrSet *string `json:\"absent_or_set,omitzero\"`")
	requireLine(t, source, "NullOrSet *string `json:\"null_or_set\"`")
	requireLine(t, source, "AbsentOrNullOrSet Optional[string] `json:\"absent_or_null_or_set,omitzero\"`")
	if !strings.Contains(source, "type Optional[T any] struct") {
		t.Fatalf("Optional is used but not declared:\n%s", source)
	}
}

// Optional is only declared when the contract needs it.
func TestOptionalIsNotDeclaredWhenUnused(t *testing.T) {
	source := generated(t,
		[]*resources.RunnableField{{Name: "note", Type: resources.RunnableFieldString, Nullable: true}},
		[]*resources.RunnableField{field("total", resources.RunnableFieldInteger)},
	)
	if strings.Contains(source, "Optional") {
		t.Fatalf("Optional declared for a contract that has no optional-and-nullable field:\n%s", source)
	}
	if strings.Contains(source, "encoding/json") {
		t.Fatalf("bindings import encoding/json without Optional:\n%s", source)
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

	requireLine(t, source, "Options *InputOptions `json:\"options,omitzero\"`")
	requireLine(t, source, "StopWords *[]string `json:\"stop_words\"`")
	requireLine(t, source, "Records []InputRecordsItem `json:\"records\"`")
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
	requireLine(t, source, "Scores []*int64 `json:\"scores\"`")
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
