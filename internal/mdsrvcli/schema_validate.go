package mdsrvcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

const mdsrvJobSchemaURL = "https://headlessmolstar.local/schema/mdsrv-job-v1.json"

func validateMDSrvJobSchemaFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return validateMDSrvJobSchemaBytes(data, path)
}

func validateMDSrvJobSchemaBytes(data []byte, name string) error {
	instance, err := mdsrvSchemaInstance(data, name)
	if err != nil {
		return err
	}
	schemaDoc, err := mdsrvSchemaDocument()
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(mdsrvJobSchemaURL, schemaDoc); err != nil {
		return err
	}
	schema, err := compiler.Compile(mdsrvJobSchemaURL)
	if err != nil {
		return err
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	return nil
}

func mdsrvSchemaDocument() (any, error) {
	data, err := json.Marshal(manifestSchema())
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

func mdsrvSchemaInstance(data []byte, name string) (any, error) {
	if isJSONDocumentName(name) || json.Valid(data) {
		return jsonschema.UnmarshalJSON(bytes.NewReader(data))
	}
	var decoded any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode yaml: %w", err)
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(normalized))
}

func isJSONDocumentName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".json" || ext == ".jsonl"
}

func isJSONPath(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".json" || ext == ".jsonl"
}
