package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	toolSchemaOnce   sync.Once
	toolSchemaErr    error
	toolSchemaByName map[string]*jsonschema.Schema
)

func compileToolSchemas() {
	toolSchemaByName = map[string]*jsonschema.Schema{}
	for _, def := range toolDefinitions() {
		compiler := jsonschema.NewCompiler()
		schemaJSON, err := json.Marshal(def.InputSchema)
		if err != nil {
			toolSchemaErr = err
			return
		}
		schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err != nil {
			toolSchemaErr = err
			return
		}
		if err := compiler.AddResource("tool://"+def.Name, schemaDoc); err != nil {
			toolSchemaErr = err
			return
		}
		schema, err := compiler.Compile("tool://" + def.Name)
		if err != nil {
			toolSchemaErr = err
			return
		}
		toolSchemaByName[def.Name] = schema
	}
}

func validateToolArguments(toolName string, argsRaw json.RawMessage) error {
	toolSchemaOnce.Do(compileToolSchemas)
	if toolSchemaErr != nil {
		return toolSchemaErr
	}
	schema := toolSchemaByName[toolName]
	if schema == nil {
		return fmt.Errorf("missing schema for tool %s", toolName)
	}

	var value any = map[string]any{}
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &value); err != nil {
			return err
		}
	}
	return schema.Validate(value)
}
