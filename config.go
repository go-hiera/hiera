// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"fmt"

	"github.com/go-ruby-yaml/yaml"
)

// Config is a parsed Hiera 5 configuration (hiera.yaml).
type Config struct {
	Version   int
	Defaults  Defaults
	Hierarchy []HierarchyEntry
	dir       string // directory of hiera.yaml; base for relative datadirs
}

// Dir reports the directory the configuration was loaded from.
func (c *Config) Dir() string { return c.dir }

// Defaults holds the config-level defaults inherited by hierarchy levels.
type Defaults struct {
	DataDir     string
	BackendKind string // "", "data_hash", "data_dig", "lookup_key"
	BackendName string
	Options     map[string]any
}

// HierarchyEntry is one level of the hierarchy.
type HierarchyEntry struct {
	Name        string
	Paths       []string // from path / paths, relative to the datadir
	Globs       []string // from glob / globs, relative to the datadir
	MappedPaths []string // exactly 3 elements: [scope_key, loop_var, template]
	DataDir     string
	BackendKind string // "", "data_hash", "data_dig", "lookup_key"
	BackendName string
	Options     map[string]any
}

// ParseConfig parses hiera.yaml bytes; dir is the directory containing it.
func ParseConfig(data []byte, dir string) (*Config, error) {
	v, err := yaml.Load(string(data))
	if err != nil {
		return nil, fmt.Errorf("hiera.yaml: %w", err)
	}
	m, ok := normalize(v).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hiera.yaml: top level must be a mapping")
	}
	cfg := &Config{dir: dir}

	rawVer, ok := m["version"]
	if !ok {
		return nil, fmt.Errorf("hiera.yaml: missing 'version'")
	}
	ver, ok := toInt(rawVer)
	if !ok {
		return nil, fmt.Errorf("hiera.yaml: 'version' must be an integer, got %T", rawVer)
	}
	if ver != 5 {
		return nil, fmt.Errorf("hiera.yaml: unsupported version %d (only version 5 is supported)", ver)
	}
	cfg.Version = 5

	if _, ok := m["default_hierarchy"]; ok {
		return nil, fmt.Errorf("hiera.yaml: 'default_hierarchy' is not supported in v0.1")
	}

	if raw, ok := m["defaults"]; ok {
		dm, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("hiera.yaml: 'defaults' must be a mapping, got %T", raw)
		}
		d, err := parseDefaults(dm)
		if err != nil {
			return nil, err
		}
		cfg.Defaults = d
	}

	rawHier, ok := m["hierarchy"]
	if !ok {
		return nil, fmt.Errorf("hiera.yaml: missing 'hierarchy'")
	}
	list, ok := rawHier.([]any)
	if !ok {
		return nil, fmt.Errorf("hiera.yaml: 'hierarchy' must be a list, got %T", rawHier)
	}
	for _, item := range list {
		e, err := parseEntry(item)
		if err != nil {
			return nil, err
		}
		cfg.Hierarchy = append(cfg.Hierarchy, e)
	}
	return cfg, nil
}

// parseDefaults reads the defaults mapping.
func parseDefaults(m map[string]any) (Defaults, error) {
	var d Defaults
	for k, v := range m {
		switch k {
		case "datadir":
			s, ok := v.(string)
			if !ok {
				return d, fmt.Errorf("hiera.yaml: defaults.datadir must be a string")
			}
			d.DataDir = s
		case "data_hash", "data_dig", "lookup_key":
			s, ok := v.(string)
			if !ok {
				return d, fmt.Errorf("hiera.yaml: defaults.%s must be a string", k)
			}
			if d.BackendKind != "" {
				return d, fmt.Errorf("hiera.yaml: defaults declares multiple backend functions")
			}
			d.BackendKind, d.BackendName = k, s
		case "options":
			om, ok := v.(map[string]any)
			if !ok {
				return d, fmt.Errorf("hiera.yaml: defaults.options must be a mapping")
			}
			d.Options = om
		default:
			return d, fmt.Errorf("hiera.yaml: unknown key %q in defaults", k)
		}
	}
	return d, nil
}

// parseEntry reads one hierarchy entry.
func parseEntry(item any) (HierarchyEntry, error) {
	var e HierarchyEntry
	m, ok := item.(map[string]any)
	if !ok {
		return e, fmt.Errorf("hiera.yaml: hierarchy entry must be a mapping, got %T", item)
	}
	for k, v := range m {
		switch k {
		case "name":
			s, ok := v.(string)
			if !ok {
				return e, fmt.Errorf("hiera.yaml: hierarchy entry 'name' must be a string")
			}
			e.Name = s
		case "path":
			s, ok := v.(string)
			if !ok {
				return e, fmt.Errorf("hiera.yaml: 'path' must be a string in entry %q", e.Name)
			}
			e.Paths = append(e.Paths, s)
		case "paths":
			ss, err := asStringList(v)
			if err != nil {
				return e, fmt.Errorf("hiera.yaml: 'paths' in entry %q: %w", e.Name, err)
			}
			e.Paths = append(e.Paths, ss...)
		case "glob":
			s, ok := v.(string)
			if !ok {
				return e, fmt.Errorf("hiera.yaml: 'glob' must be a string in entry %q", e.Name)
			}
			e.Globs = append(e.Globs, s)
		case "globs":
			ss, err := asStringList(v)
			if err != nil {
				return e, fmt.Errorf("hiera.yaml: 'globs' in entry %q: %w", e.Name, err)
			}
			e.Globs = append(e.Globs, ss...)
		case "mapped_paths":
			ss, err := asStringList(v)
			if err != nil {
				return e, fmt.Errorf("hiera.yaml: 'mapped_paths' in entry %q: %w", e.Name, err)
			}
			if len(ss) != 3 {
				return e, fmt.Errorf("hiera.yaml: 'mapped_paths' in entry %q needs exactly 3 elements", e.Name)
			}
			e.MappedPaths = ss
		case "datadir":
			s, ok := v.(string)
			if !ok {
				return e, fmt.Errorf("hiera.yaml: 'datadir' must be a string in entry %q", e.Name)
			}
			e.DataDir = s
		case "data_hash", "data_dig", "lookup_key":
			s, ok := v.(string)
			if !ok {
				return e, fmt.Errorf("hiera.yaml: '%s' must be a string in entry %q", k, e.Name)
			}
			if e.BackendKind != "" {
				return e, fmt.Errorf("hiera.yaml: entry %q declares multiple backend functions", e.Name)
			}
			e.BackendKind, e.BackendName = k, s
		case "options":
			om, ok := v.(map[string]any)
			if !ok {
				return e, fmt.Errorf("hiera.yaml: 'options' must be a mapping in entry %q", e.Name)
			}
			e.Options = om
		default:
			return e, fmt.Errorf("hiera.yaml: unknown key %q in hierarchy entry %q", k, e.Name)
		}
	}
	if e.Name == "" {
		return e, fmt.Errorf("hiera.yaml: a hierarchy entry is missing 'name'")
	}
	if len(e.Paths)+len(e.Globs)+len(e.MappedPaths) == 0 {
		return e, fmt.Errorf("hiera.yaml: entry %q has no path/paths/glob/globs/mapped_paths", e.Name)
	}
	return e, nil
}

// asStringList coerces a YAML string or list-of-strings to []string.
func asStringList(v any) ([]string, error) {
	switch x := v.(type) {
	case string:
		return []string{x}, nil
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("expected string, got %T", e)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a string or a list of strings, got %T", v)
	}
}

// toInt coerces YAML/JSON integer representations to an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
		return 0, false
	default:
		return 0, false
	}
}
