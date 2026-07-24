package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	configschema "github.com/KrzysztofHajdamowicz/gbbconnect-go/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

const configSchemaURL = "https://github.com/KrzysztofHajdamowicz/gbbconnect-go/schema/gbbconnect.schema.json"

var compiledConfigSchema = sync.OnceValues(compileConfigSchema)

// ValidateSchemaFile validates the unmodified YAML or JSON document so unknown
// fields and input types cannot be hidden by decoding into Config first.
func ValidateSchemaFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read configuration for JSON Schema validation %q: %w", path, err)
	}

	var value any
	if strings.EqualFold(filepath.Ext(path), ".json") {
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("parse JSON for JSON Schema validation %q: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("parse YAML for JSON Schema validation %q: %w", path, err)
		}
	}
	return ValidateSchema(value)
}

// ValidateSchema validates a JSON-compatible value against the published
// configuration schema.
func ValidateSchema(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode value for JSON Schema validation: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("decode value for JSON Schema validation: %w", err)
	}

	schema, err := compiledConfigSchema()
	if err != nil {
		return err
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("JSON Schema validation failed: %w", err)
	}
	return nil
}

func compileConfigSchema() (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(configschema.ConfigJSON))
	if err != nil {
		return nil, fmt.Errorf("decode embedded configuration schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(configSchemaURL, document); err != nil {
		return nil, fmt.Errorf("register embedded configuration schema: %w", err)
	}
	schema, err := compiler.Compile(configSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile embedded configuration schema: %w", err)
	}
	return schema, nil
}
