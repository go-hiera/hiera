// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"strconv"
	"strings"
)

// Scope is the pluggable variable provider the engine resolves interpolation
// against. Lookup receives the raw reference used inside %{...} or as the
// argument to scope() — a bare name ("certname") or a dotted path
// ("facts.os.name", "trusted.certname") — and returns the value and whether it
// is present. This is the seam a facts engine (go-facter) or the Ruby binding
// (go-ruby-hiera) implements to feed facts / trusted / server_facts data in.
type Scope interface {
	Lookup(ref string) (Value, bool)
}

// MapScope is a [Scope] backed by a nested map of variables. A dotted
// reference digs through nested map[string]any and []any (numeric index)
// values, so facts such as {"os": {"name": "Debian"}} resolve for
// "facts.os.name" when stored under the "facts" key. A leading "::" is
// ignored, matching Puppet's top-scope syntax.
type MapScope map[string]Value

// Lookup implements [Scope].
func (s MapScope) Lookup(ref string) (Value, bool) {
	return digPath(map[string]any(s), splitKey(ref))
}

// splitKey splits a dotted reference into segments, dropping a leading "::".
func splitKey(ref string) []string {
	ref = strings.TrimPrefix(ref, "::")
	return strings.Split(ref, ".")
}

// digPath walks v following segs, indexing into map[string]any by key and into
// []any by decimal index, and reports the reached value and whether the whole
// path resolved.
func digPath(v Value, segs []string) (Value, bool) {
	cur := v
	for _, seg := range segs {
		switch c := cur.(type) {
		case map[string]any:
			nv, ok := c[seg]
			if !ok {
				return nil, false
			}
			cur = nv
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}
