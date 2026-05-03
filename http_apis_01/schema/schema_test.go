package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TODO: look into creating a SchemaCache like in https://github.com/modelcontextprotocol/go-sdk/blob/v1.6.0/mcp/schema_cache.go

func buildSchemaFromGoType[T any]() (*jsonschema.Resolved, error) {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, err
	}
	rs, err := schema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	return rs, err
}

func printSchema(t *testing.T, schema *jsonschema.Schema) {
	repr, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Logf("failed to marshal schema: %v\n", err)
		return
	}
	t.Logf("Schema is:\n%s\n", repr)
}

type Person struct {
	Name string `json:"name" jsonschema:"person's full name"`
	Age  int    `json:"age,omitzero"` // Note: a tag like `validate:"gt=0,lt=100"` doesn't get recognised
}

func TestPerson01(t *testing.T) {
	p := map[string]any{"name": "John Doe", "age": 20}

	rs, err := buildSchemaFromGoType[Person]()
	if err != nil {
		t.Errorf("failed to create schema for the Person type: %v\n", err)
	}

	if err := rs.Validate(p); err != nil {
		printSchema(t, rs.Schema())
		t.Errorf("validation failed: %v\n", err)
	}
}

func TestPerson02(t *testing.T) {
	p := map[string]any{"name": "John Doe", "age": "abc"}

	rs, err := buildSchemaFromGoType[Person]()
	if err != nil {
		t.Errorf("failed to create schema for the Person type: %v\n", err)
	}

	err = rs.Validate(p)
	switch err {
	case nil:
		t.Errorf("error check failed: wanted not nil, got nil")
	default:
		sentinel := "has type \"string\", want \"integer\""
		if !strings.Contains(err.Error(), sentinel) {
			printSchema(t, rs.Schema())
			t.Errorf("error check failed: wanted error to contain '%s'\n", sentinel)
		}
	}
}
