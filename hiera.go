// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package hiera is a pure-Go (CGO-free) reimplementation of Puppet's Hiera 5
// hierarchical data-lookup engine.
//
// It loads a Hiera 5 configuration (hiera.yaml), walks the configured
// hierarchy of data sources, and resolves a key to a typed value, honouring
// Hiera's merge behaviours (first / unique / hash / deep), per-key
// lookup_options, dotted-key digging into structured data, and the full
// %{...} interpolation grammar (scope / facts / trusted variables plus the
// scope, lookup, hiera, alias, literal functions) with interpolation-loop
// detection. It is faithful to Hiera 5 semantics so it can back Puppet's
// lookup() function.
//
// # Composition
//
// The engine does not resolve fact/variable references itself: the caller
// supplies a [Scope] (a pluggable variable provider). This is the seam a
// facts engine such as go-facter, or the Ruby binding go-ruby-hiera, plugs
// into — %{facts.os.name}, %{trusted.certname} and %{scope('x')} are resolved
// against the injected Scope. The engine therefore has no hard dependency on
// any particular fact source.
//
// # Value model
//
// Data files are decoded to a small, fixed set of Go types so a host can map
// results onto its own object graph:
//
//	Hash    -> map[string]any
//	Array   -> []any
//	String  -> string
//	Integer -> int64
//	Float   -> float64
//	Boolean -> bool
//	Null    -> nil
//
// YAML is decoded through github.com/go-ruby-yaml/yaml (the ecosystem's
// Ruby-faithful, pure-Go Psych backend, matching the YAML semantics Puppet
// relies on); JSON through the standard library.
package hiera

import (
	"os"
	"path/filepath"
)

// Value is any value the engine handles; it is documentary — the public API
// uses any.
type Value = any

// Hiera is a configured lookup engine: a parsed configuration bound to a
// Scope and a set of data-source backends.
type Hiera struct {
	config  *Config
	scope   Scope
	parsers map[string]dataParser
}

// New returns a lookup engine for cfg resolving interpolation against scope.
// The built-in yaml_data and json_data backends are registered; register more
// with [Hiera.RegisterDataHash]. New panics if cfg or scope is nil.
func New(cfg *Config, scope Scope) *Hiera {
	if cfg == nil {
		panic("hiera: nil config")
	}
	if scope == nil {
		panic("hiera: nil scope")
	}
	h := &Hiera{config: cfg, scope: scope, parsers: map[string]dataParser{}}
	for name, p := range builtinParsers {
		h.parsers[name] = p
	}
	return h
}

// Load reads and parses the hiera.yaml at configPath and returns an engine
// resolving interpolation against scope. The configuration's directory is the
// base for resolving relative datadirs.
func Load(configPath string, scope Scope) (*Hiera, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	cfg, err := ParseConfig(data, filepath.Dir(configPath))
	if err != nil {
		return nil, err
	}
	return New(cfg, scope), nil
}

// Config returns the engine's parsed configuration.
func (h *Hiera) Config() *Config { return h.config }

// RegisterDataHash registers (or replaces) a data_hash backend under name so a
// hierarchy level may select it via "data_hash: name". A backend parses the
// raw bytes of one data file into a hash; path is supplied for error context.
func (h *Hiera) RegisterDataHash(name string, fn func(data []byte, path string) (map[string]any, error)) {
	h.parsers[name] = fn
}
