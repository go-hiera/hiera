// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"fmt"
	"sort"
)

// MergeKind is a Hiera merge behaviour.
type MergeKind int

const (
	// MergeFirst returns the highest-priority value found (the default).
	MergeFirst MergeKind = iota
	// MergeUnique flattens every found array/scalar into a single
	// deduplicated array.
	MergeUnique
	// MergeHash shallow-merges every found hash, higher priority winning.
	MergeHash
	// MergeDeep recursively merges every found hash, higher priority winning.
	MergeDeep
)

// MergeStrategy selects a merge behaviour and its options.
type MergeStrategy struct {
	Kind             MergeKind
	KnockoutPrefix   string // deep: a prefixed array element removes the plain one
	MergeHashArrays  bool   // deep: merge arrays element-wise instead of unioning
	SortMergedArrays bool   // unique/deep: sort merged arrays
}

// parseKind maps a Hiera strategy name to a MergeKind.
func parseKind(s string) (MergeKind, error) {
	switch s {
	case "first":
		return MergeFirst, nil
	case "unique":
		return MergeUnique, nil
	case "hash":
		return MergeHash, nil
	case "deep":
		return MergeDeep, nil
	default:
		return 0, fmt.Errorf("hiera: unknown merge strategy %q", s)
	}
}

// mergeFromAny builds a MergeStrategy from a lookup_options merge specification,
// which may be a bare strategy string, a {"merge": ...} wrapper, or a
// {"strategy": ..., <options>} mapping.
func mergeFromAny(v any) (MergeStrategy, error) {
	switch x := v.(type) {
	case string:
		k, err := parseKind(x)
		return MergeStrategy{Kind: k}, err
	case map[string]any:
		if inner, ok := x["merge"]; ok {
			return mergeFromAny(inner)
		}
		rawStrat, ok := x["strategy"]
		if !ok {
			return MergeStrategy{}, fmt.Errorf("hiera: merge mapping is missing 'strategy'")
		}
		name, ok := rawStrat.(string)
		if !ok {
			return MergeStrategy{}, fmt.Errorf("hiera: merge 'strategy' must be a string")
		}
		k, err := parseKind(name)
		if err != nil {
			return MergeStrategy{}, err
		}
		s := MergeStrategy{Kind: k}
		if kp, ok := x["knockout_prefix"].(string); ok {
			s.KnockoutPrefix = kp
		}
		if b, ok := x["merge_hash_arrays"].(bool); ok {
			s.MergeHashArrays = b
		}
		if b, ok := x["sort_merged_arrays"].(bool); ok {
			s.SortMergedArrays = b
		}
		return s, nil
	default:
		return MergeStrategy{}, fmt.Errorf("hiera: unsupported merge specification of type %T", v)
	}
}

// mergeValues combines the per-level values (highest priority first) per s.
func mergeValues(values []Value, s MergeStrategy) (Value, error) {
	switch s.Kind {
	case MergeFirst:
		return values[0], nil
	case MergeUnique:
		return mergeUnique(values, s)
	case MergeHash:
		return mergeHash(values)
	default: // MergeDeep
		return mergeDeep(values, s)
	}
}

// mergeUnique deep-flattens every value into one deduplicated array. Hash
// values are rejected (Hiera's unique merge is array-only).
func mergeUnique(values []Value, s MergeStrategy) (Value, error) {
	out := []any{}
	seen := map[string]bool{}
	var add func(v any) error
	add = func(v any) error {
		if arr, ok := v.([]any); ok {
			for _, e := range arr {
				if err := add(e); err != nil {
					return err
				}
			}
			return nil
		}
		if _, isMap := v.(map[string]any); isMap {
			return fmt.Errorf("hiera: unique merge cannot merge hash values")
		}
		k := scalarKey(v)
		if !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
		return nil
	}
	for _, v := range values {
		if err := add(v); err != nil {
			return nil, err
		}
	}
	if s.SortMergedArrays {
		sortValues(out)
	}
	return out, nil
}

// mergeHash shallow-merges hashes with higher-priority values winning.
func mergeHash(values []Value) (Value, error) {
	out := map[string]any{}
	for _, v := range values {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("hiera: hash merge requires hash values, got %T", v)
		}
		for k, val := range m {
			if _, exists := out[k]; !exists {
				out[k] = val
			}
		}
	}
	return out, nil
}

// mergeDeep recursively merges the values, folding from the lowest priority up
// so higher-priority values win conflicts, then applies knockout and sorting.
func mergeDeep(values []Value, s MergeStrategy) (Value, error) {
	acc := values[len(values)-1]
	for i := len(values) - 2; i >= 0; i-- {
		acc = deepMerge(values[i], acc, s)
	}
	acc = finalizeDeep(acc, s)
	return acc, nil
}

// deepMerge merges src (higher priority) over dst.
func deepMerge(src, dst any, s MergeStrategy) any {
	sm, sok := src.(map[string]any)
	dm, dok := dst.(map[string]any)
	if sok && dok {
		out := make(map[string]any, len(dm))
		for k, v := range dm {
			out[k] = v
		}
		for k, sv := range sm {
			if dv, ok := out[k]; ok {
				out[k] = deepMerge(sv, dv, s)
			} else {
				out[k] = sv
			}
		}
		return out
	}
	sa, sok2 := src.([]any)
	da, dok2 := dst.([]any)
	if sok2 && dok2 {
		if s.MergeHashArrays {
			n := len(sa)
			if len(da) > n {
				n = len(da)
			}
			out := make([]any, 0, n)
			for i := 0; i < n; i++ {
				switch {
				case i < len(sa) && i < len(da):
					out = append(out, deepMerge(sa[i], da[i], s))
				case i < len(sa):
					out = append(out, sa[i])
				default:
					out = append(out, da[i])
				}
			}
			return out
		}
		// Default: union src ahead of dst with knockout + dedup.
		combined := make([]any, 0, len(sa)+len(da))
		combined = append(combined, sa...)
		combined = append(combined, da...)
		return dedupKnockout(combined, s)
	}
	// Scalar or type mismatch: higher priority wins.
	return src
}

// dedupKnockout deduplicates arr, and when a knockout prefix is set drops both
// the "<prefix>x" markers and any plain "x" they knock out.
func dedupKnockout(arr []any, s MergeStrategy) []any {
	knocked := map[string]bool{}
	if s.KnockoutPrefix != "" {
		for _, e := range arr {
			if str, ok := e.(string); ok && hasPrefix(str, s.KnockoutPrefix) {
				knocked[str[len(s.KnockoutPrefix):]] = true
			}
		}
	}
	seen := map[string]bool{}
	out := []any{}
	for _, e := range arr {
		if str, ok := e.(string); ok && s.KnockoutPrefix != "" {
			if hasPrefix(str, s.KnockoutPrefix) {
				continue // drop the marker itself
			}
			if knocked[str] {
				continue // knocked out by a marker
			}
		}
		k := scalarKey(e)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// finalizeDeep applies hash-level knockout (a value equal to the prefix deletes
// its key) and optional array sorting to the merged tree.
func finalizeDeep(v any, s MergeStrategy) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if s.KnockoutPrefix != "" {
				if str, ok := val.(string); ok && str == s.KnockoutPrefix {
					continue
				}
			}
			out[k] = finalizeDeep(val, s)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = finalizeDeep(x[i], s)
		}
		if s.SortMergedArrays {
			sortValues(out)
		}
		return out
	default:
		return v
	}
}

// scalarKey is a type-qualified string key so distinct-typed scalars with the
// same rendering do not collide during deduplication.
func scalarKey(v any) string { return fmt.Sprintf("%T\x00%v", v, v) }

// hasPrefix reports whether s begins with prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// sortValues sorts a scalar array by rendered form for deterministic output.
func sortValues(a []any) {
	sort.SliceStable(a, func(i, j int) bool {
		return fmt.Sprintf("%v", a[i]) < fmt.Sprintf("%v", a[j])
	})
}
