// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/go-ruby-yaml/yaml"
)

// dataParser decodes the raw bytes of one data file into a hash. path is
// supplied only for error context.
type dataParser func(data []byte, path string) (map[string]any, error)

// builtinParsers holds the data_hash backends every engine starts with.
var builtinParsers = map[string]dataParser{
	"yaml_data": parseYAML,
	"json_data": parseJSON,
}

// parseYAML decodes a YAML data file through the Ruby-faithful go-ruby-yaml
// backend. An empty document is an empty hash; a non-mapping top level is an
// error (Hiera data files are always hashes).
func parseYAML(data []byte, path string) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	v, err := yaml.Load(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return topHash(normalize(v), path)
}

// parseJSON decodes a JSON data file through the standard library.
func parseJSON(data []byte, path string) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return topHash(normalize(v), path)
}

// topHash asserts that a decoded document is a mapping.
func topHash(v any, path string) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: top level is not a mapping (got %T)", path, v)
	}
	return m, nil
}

// normalize converts a decoded value tree (which may contain go-ruby-yaml's
// *yaml.Map / yaml.Symbol, or the standard library's map[string]any) into the
// engine's canonical model: map[string]any for hashes, []any for arrays,
// strings for symbols, and scalars unchanged.
func normalize(v any) any {
	switch x := v.(type) {
	case *yaml.Map:
		m := make(map[string]any, x.Len())
		for _, p := range x.Pairs() {
			m[keyString(p.Key)] = normalize(p.Val)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = normalize(val)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i := range x {
			s[i] = normalize(x[i])
		}
		return s
	case yaml.Symbol:
		return string(x)
	default:
		return v
	}
}

// keyString renders a decoded mapping key as the string Hiera keys always are.
func keyString(k any) string {
	switch s := k.(type) {
	case string:
		return s
	case yaml.Symbol:
		return string(s)
	default:
		return fmt.Sprintf("%v", k)
	}
}
