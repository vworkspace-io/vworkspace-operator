/*
Copyright 2026 vWorkspace Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
)

var compiledSchemas sync.Map // map[string]*jsonschema.Schema

// ValidateParameters checks Operation.spec.parameters against the built-in template inputSchema.
func ValidateParameters(typ opsv1alpha1.OperationType, engine opsv1alpha1.OperationEngine, raw []byte) error {
	tpl, ok := Lookup(typ, engine)
	if !ok {
		return nil
	}
	schemaDoc, ok := InputSchema(tpl.Ref)
	if !ok {
		return nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	schema, err := compiledInputSchema(tpl.Ref, schemaDoc)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("decode parameters for template %s: %w", tpl.Ref, err)
	}
	if err := schema.Validate(doc); err != nil {
		return fmt.Errorf("parameters invalid for template %s: %w", tpl.Ref, err)
	}
	return nil
}

func compiledInputSchema(ref, schemaDoc string) (*jsonschema.Schema, error) {
	if cached, ok := compiledSchemas.Load(ref); ok {
		return cached.(*jsonschema.Schema), nil
	}
	var doc any
	if err := json.Unmarshal([]byte(schemaDoc), &doc); err != nil {
		return nil, fmt.Errorf("decode parameters schema for template %s: %w", ref, err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(ref, doc); err != nil {
		return nil, fmt.Errorf("compile parameters schema for template %s: %w", ref, err)
	}
	schema, err := compiler.Compile(ref)
	if err != nil {
		return nil, fmt.Errorf("compile parameters schema for template %s: %w", ref, err)
	}
	compiledSchemas.Store(ref, schema)
	return schema, nil
}
