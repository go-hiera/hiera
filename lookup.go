// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Options controls a single [Hiera.Lookup] call. A nil *Options is equivalent
// to the zero value: the merge behaviour comes from lookup_options (falling
// back to "first") and there is no default.
type Options struct {
	// Merge, when non-nil, overrides the merge behaviour for this call.
	Merge *MergeStrategy
	// Default supplies a default_value returned when the key is not found.
	Default Value
	// HasDefault enables Default (so a nil default value is distinguishable).
	HasDefault bool
	// DefaultValuesHash supplies per-key defaults consulted, by the looked-up
	// root key, when the key is not found and HasDefault is false.
	DefaultValuesHash map[string]any
}

// Lookup resolves key and returns its value, whether it was found, and any
// error. key may be dotted (e.g. "profile.ntp.servers.0") to dig into
// structured data after the root key is looked up and merged.
func (h *Hiera) Lookup(key string, opts *Options) (Value, bool, error) {
	ctx := &interpCtx{scope: h.scope}
	return h.lookup(key, opts, ctx)
}

// lookup is the recursion-aware core shared by Lookup and interpolation.
func (h *Hiera) lookup(key string, opts *Options, ctx *interpCtx) (Value, bool, error) {
	pop, err := ctx.enter(key)
	if err != nil {
		return nil, false, err
	}
	defer pop()

	segs := strings.Split(key, ".")
	rootKey, subPath := segs[0], segs[1:]

	levels, err := h.loadLevels(ctx)
	if err != nil {
		return nil, false, err
	}

	strat, err := h.strategyFor(rootKey, opts, levels)
	if err != nil {
		return nil, false, err
	}

	values, err := h.gather(rootKey, strat.Kind, levels, ctx)
	if err != nil {
		return nil, false, err
	}

	var result Value
	found := len(values) > 0
	switch {
	case found:
		result, err = mergeValues(values, strat)
		if err != nil {
			return nil, false, err
		}
	case opts != nil && opts.HasDefault:
		result, err = h.interpolate(opts.Default, ctx)
		if err != nil {
			return nil, false, err
		}
		found = true
	case opts != nil && opts.DefaultValuesHash != nil:
		if dv, ok := opts.DefaultValuesHash[rootKey]; ok {
			result, found = dv, true
		}
	}
	if !found {
		return nil, false, nil
	}
	if len(subPath) > 0 {
		dug, ok := digPath(result, subPath)
		if !ok {
			return nil, false, nil
		}
		result = dug
	}
	return result, true, nil
}

// loadLevels resolves and parses every existing data file across the
// hierarchy once, in priority order, so a single lookup shares the loaded data
// between lookup_options and the value gather. Missing files are skipped.
func (h *Hiera) loadLevels(ctx *interpCtx) ([]map[string]any, error) {
	var levels []map[string]any
	for _, entry := range h.config.Hierarchy {
		paths, err := h.resolvePaths(entry, ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			data, ok, err := h.loadData(entry, p)
			if err != nil {
				return nil, err
			}
			if ok {
				levels = append(levels, data)
			}
		}
	}
	return levels, nil
}

// strategyFor picks the merge strategy for rootKey: an explicit override wins,
// then a matching lookup_options entry, else "first".
func (h *Hiera) strategyFor(rootKey string, opts *Options, levels []map[string]any) (MergeStrategy, error) {
	if opts != nil && opts.Merge != nil {
		return *opts.Merge, nil
	}
	lo, err := lookupOptions(levels)
	if err != nil {
		return MergeStrategy{}, err
	}
	if spec, ok := matchLookupOption(lo, rootKey); ok {
		return mergeFromAny(spec)
	}
	return MergeStrategy{Kind: MergeFirst}, nil
}

// gather collects the interpolated values of rootKey across the loaded levels
// in priority order. For a first-wins merge it stops at the first match.
func (h *Hiera) gather(rootKey string, kind MergeKind, levels []map[string]any, ctx *interpCtx) ([]Value, error) {
	var values []Value
	for _, data := range levels {
		raw, ok := data[rootKey]
		if !ok {
			continue
		}
		v, err := h.interpolate(raw, ctx)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
		if kind == MergeFirst {
			return values, nil
		}
	}
	return values, nil
}

// lookupOptions hash-merges (higher priority winning) the lookup_options hash
// across the loaded levels.
func lookupOptions(levels []map[string]any) (map[string]any, error) {
	result := map[string]any{}
	for _, data := range levels {
		raw, ok := data["lookup_options"]
		if !ok {
			continue
		}
		lom, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("hiera: lookup_options must be a mapping, got %T", raw)
		}
		for k, v := range lom {
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
	}
	return result, nil
}

// matchLookupOption finds the merge spec for key: an exact entry, else a
// "^regex" entry whose pattern matches.
func matchLookupOption(lo map[string]any, key string) (any, bool) {
	if v, ok := lo[key]; ok {
		return v, true
	}
	for k, v := range lo {
		if strings.HasPrefix(k, "^") {
			if re, err := regexp.Compile(k); err == nil && re.MatchString(key) {
				return v, true
			}
		}
	}
	return nil, false
}

// resolvePaths expands a hierarchy entry into the ordered list of candidate
// data-file paths, interpolating scope variables and resolving globs and
// mapped_paths.
func (h *Hiera) resolvePaths(entry HierarchyEntry, ctx *interpCtx) ([]string, error) {
	base := h.datadir(entry)
	var out []string

	for _, p := range entry.Paths {
		s, err := h.interpolateStr(p, ctx, true)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.Join(base, stringify(s)))
	}

	for _, g := range entry.Globs {
		s, err := h.interpolateStr(g, ctx, true)
		if err != nil {
			return nil, err
		}
		matches, err := filepath.Glob(filepath.Join(base, stringify(s)))
		if err != nil {
			return nil, fmt.Errorf("hiera: glob %q in entry %q: %w", g, entry.Name, err)
		}
		out = append(out, matches...)
	}

	if entry.MappedPaths != nil {
		mp, err := h.resolveMapped(entry, base, ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, mp...)
	}
	return out, nil
}

// resolveMapped expands a mapped_paths entry: [scope_key, loop_var, template].
// The scope key must resolve to an array; each element is bound to loop_var and
// the template is interpolated. A missing or non-array scope key yields no
// paths.
func (h *Hiera) resolveMapped(entry HierarchyEntry, base string, ctx *interpCtx) ([]string, error) {
	scopeKey, loopVar, template := entry.MappedPaths[0], entry.MappedPaths[1], entry.MappedPaths[2]
	raw, ok := ctx.scope.Lookup(scopeKey)
	if !ok {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, nil
	}
	var out []string
	for _, elem := range arr {
		child := ctx.withScope(mappedScope{name: loopVar, val: elem, base: ctx.scope})
		s, err := h.interpolateStr(template, child, true)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.Join(base, stringify(s)))
	}
	return out, nil
}

// datadir resolves the absolute data directory for an entry, applying the
// entry-level datadir, then the config default, then "data", relative to the
// configuration's directory.
func (h *Hiera) datadir(entry HierarchyEntry) string {
	dir := entry.DataDir
	if dir == "" {
		dir = h.config.Defaults.DataDir
	}
	if dir == "" {
		dir = "data"
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(h.config.dir, dir)
}

// loadData reads and parses one candidate file. A missing file is reported as
// not-present (not an error); any other read or parse failure is an error.
func (h *Hiera) loadData(entry HierarchyEntry, path string) (map[string]any, bool, error) {
	kind, name := h.resolveBackend(entry)
	parser, err := h.parserFor(kind, name)
	if err != nil {
		return nil, false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	m, err := parser(raw, path)
	if err != nil {
		return nil, false, err
	}
	return m, true, nil
}

// resolveBackend returns the backend kind and name for an entry, inheriting the
// config default and finally yaml_data.
func (h *Hiera) resolveBackend(entry HierarchyEntry) (kind, name string) {
	if entry.BackendKind != "" {
		return entry.BackendKind, entry.BackendName
	}
	if h.config.Defaults.BackendKind != "" {
		return h.config.Defaults.BackendKind, h.config.Defaults.BackendName
	}
	return "data_hash", "yaml_data"
}

// parserFor returns the registered data_hash parser, erroring for unknown
// backends and for the data_dig/lookup_key kinds not implemented in v0.1.
func (h *Hiera) parserFor(kind, name string) (dataParser, error) {
	if kind != "data_hash" {
		return nil, fmt.Errorf("hiera: backend kind %q is not supported (v0.1 implements data_hash only)", kind)
	}
	if p, ok := h.parsers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("hiera: unknown data_hash backend %q", name)
}

// mappedScope overlays a single loop variable onto a base Scope for
// mapped_paths interpolation.
type mappedScope struct {
	name string
	val  any
	base Scope
}

// Lookup implements [Scope].
func (m mappedScope) Lookup(ref string) (Value, bool) {
	if ref == m.name {
		return m.val, true
	}
	if strings.HasPrefix(ref, m.name+".") {
		return digPath(m.val, splitKey(strings.TrimPrefix(ref, m.name+".")))
	}
	return m.base.Lookup(ref)
}
