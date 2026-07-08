// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"reflect"
	"testing"
)

func TestToInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{int(3), 3, true},
		{int64(4), 4, true},
		{float64(5), 5, true},
		{float64(5.5), 0, false},
		{"x", 0, false},
	}
	for _, c := range cases {
		got, ok := toInt(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("toInt(%v) = %d,%v; want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestAsStringList(t *testing.T) {
	if _, err := asStringList("s"); err != nil {
		t.Fatal(err)
	}
	if _, err := asStringList([]any{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := asStringList([]any{1}); err == nil {
		t.Fatal("expected element error")
	}
	if _, err := asStringList(5); err == nil {
		t.Fatal("expected type error")
	}
}

func TestDigPathArray(t *testing.T) {
	v := []any{"a", "b"}
	if got, ok := digPath(v, []string{"1"}); !ok || got != "b" {
		t.Fatalf("digPath = %v %v", got, ok)
	}
	if _, ok := digPath(v, []string{"-1"}); ok {
		t.Fatal("negative index should fail")
	}
}

func TestStringify(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"s", "s"},
		{true, "true"},
		{false, "false"},
		{int64(7), "7"},
		{3.5, "3.5"},
	}
	for _, c := range cases {
		if got := stringify(c.in); got != c.want {
			t.Errorf("stringify(%v) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestSplitKey(t *testing.T) {
	if got := splitKey("::facts.os"); !reflect.DeepEqual(got, []string{"facts", "os"}) {
		t.Fatalf("splitKey = %#v", got)
	}
}

func TestParseKind(t *testing.T) {
	for name, want := range map[string]MergeKind{"first": MergeFirst, "unique": MergeUnique, "hash": MergeHash, "deep": MergeDeep} {
		if k, err := parseKind(name); err != nil || k != want {
			t.Errorf("parseKind(%q) = %v %v", name, k, err)
		}
	}
	if _, err := parseKind("bogus"); err == nil {
		t.Fatal("expected unknown strategy error")
	}
}

func TestMergeFromAny(t *testing.T) {
	// map with strategy + all options
	s, err := mergeFromAny(map[string]any{
		"strategy":           "deep",
		"knockout_prefix":    "--",
		"merge_hash_arrays":  true,
		"sort_merged_arrays": true,
	})
	if err != nil || s.Kind != MergeDeep || s.KnockoutPrefix != "--" || !s.MergeHashArrays || !s.SortMergedArrays {
		t.Fatalf("mergeFromAny strategy = %#v %v", s, err)
	}
	if _, err := mergeFromAny(map[string]any{}); err == nil {
		t.Fatal("expected missing strategy error")
	}
	if _, err := mergeFromAny(map[string]any{"strategy": 5}); err == nil {
		t.Fatal("expected strategy type error")
	}
	if _, err := mergeFromAny(map[string]any{"strategy": "bogus"}); err == nil {
		t.Fatal("expected unknown strategy error")
	}
	if _, err := mergeFromAny(42); err == nil {
		t.Fatal("expected unsupported spec error")
	}
	if _, err := mergeFromAny("bogus"); err == nil {
		t.Fatal("expected unknown string strategy error")
	}
}

func TestMergeUniqueHashRejected(t *testing.T) {
	if _, err := mergeUnique([]any{map[string]any{"a": 1}}, MergeStrategy{}); err == nil {
		t.Fatal("expected hash-in-unique error")
	}
	// A hash nested inside an array must also be rejected (recursive path).
	if _, err := mergeUnique([]any{[]any{map[string]any{"a": 1}}}, MergeStrategy{}); err == nil {
		t.Fatal("expected nested hash-in-unique error")
	}
}

func TestMergeUniqueSort(t *testing.T) {
	got, err := mergeUnique([]any{[]any{"c", "a"}, "b"}, MergeStrategy{Kind: MergeUnique, SortMergedArrays: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []any{"a", "b", "c"}) {
		t.Fatalf("sorted unique = %#v", got)
	}
}

func TestMergeHashNonHashRejected(t *testing.T) {
	if _, err := mergeHash([]any{"scalar"}); err == nil {
		t.Fatal("expected non-hash error")
	}
}

func TestDeepMergeHashArrays(t *testing.T) {
	src := map[string]any{"list": []any{"a", "b", "c"}}
	dst := map[string]any{"list": []any{"x"}}
	s := MergeStrategy{Kind: MergeDeep, MergeHashArrays: true}
	got, err := mergeDeep([]any{src, dst}, s)
	if err != nil {
		t.Fatal(err)
	}
	// element-wise: index 0 -> src "a" wins, extra src tail appended.
	want := map[string]any{"list": []any{"a", "b", "c"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge_hash_arrays = %#v; want %#v", got, want)
	}
	// dst longer than src: the dst tail is appended.
	got2, err := mergeDeep([]any{
		map[string]any{"list": []any{"a"}},
		map[string]any{"list": []any{"x", "y", "z"}},
	}, s)
	if err != nil {
		t.Fatal(err)
	}
	want2 := map[string]any{"list": []any{"a", "y", "z"}}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("merge_hash_arrays dst-longer = %#v; want %#v", got2, want2)
	}
}

func TestDeepMergeArrayUnionNoKnockout(t *testing.T) {
	src := map[string]any{"list": []any{"a", "b"}}
	dst := map[string]any{"list": []any{"b", "c"}}
	got, err := mergeDeep([]any{src, dst}, MergeStrategy{Kind: MergeDeep})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"list": []any{"a", "b", "c"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("union = %#v; want %#v", got, want)
	}
}

func TestDeepMergeHashKnockout(t *testing.T) {
	// A value equal to the knockout prefix deletes its key.
	src := map[string]any{"drop": "--", "keep": 1}
	dst := map[string]any{"drop": "old", "other": 2}
	got, err := mergeDeep([]any{src, dst}, MergeStrategy{Kind: MergeDeep, KnockoutPrefix: "--"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"keep": 1, "other": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hash knockout = %#v; want %#v", got, want)
	}
}

func TestDeepMergeScalarWins(t *testing.T) {
	got, err := mergeDeep([]any{"high", "low"}, MergeStrategy{Kind: MergeDeep})
	if err != nil {
		t.Fatal(err)
	}
	if got != "high" {
		t.Fatalf("scalar merge = %v", got)
	}
}

func TestMappedScopeLookup(t *testing.T) {
	base := MapScope{"environment": "prod"}
	m := mappedScope{name: "svc", val: map[string]any{"name": "ntp"}, base: base}
	if v, ok := m.Lookup("svc"); !ok || !reflect.DeepEqual(v, map[string]any{"name": "ntp"}) {
		t.Fatalf("svc = %v %v", v, ok)
	}
	if v, ok := m.Lookup("svc.name"); !ok || v != "ntp" {
		t.Fatalf("svc.name = %v %v", v, ok)
	}
	if v, ok := m.Lookup("environment"); !ok || v != "prod" {
		t.Fatalf("delegate = %v %v", v, ok)
	}
}

func TestNormalizeSymbolValue(t *testing.T) {
	// A symbol scalar normalises to its string form.
	m, err := parseYAML([]byte("k: :sym\n"), "x.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m["k"] != "sym" {
		t.Fatalf("symbol value = %v", m["k"])
	}
}
