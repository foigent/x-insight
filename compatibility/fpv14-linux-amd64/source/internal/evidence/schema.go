package evidence

import (
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type SchemaValidator struct {
	schema *jsonschema.Schema
}

func NewSchemaValidator(schemaPath string) (*SchemaValidator, error) {
	abs, err := filepath.Abs(schemaPath)
	if err != nil {
		return nil, err
	}
	loc := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(jsonschema.SchemeURLLoader{"file": jsonschema.FileLoader{}})

	schema, err := compiler.Compile(loc)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", schemaPath, err)
	}
	return &SchemaValidator{schema: schema}, nil
}

func (v *SchemaValidator) Validate(event map[string]any) error {
	if v == nil || v.schema == nil {
		return fmt.Errorf("schema validator is not initialized")
	}
	return v.schema.Validate(event)
}
