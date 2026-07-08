// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// testScope is the fact/variable scope used by the integration fixtures.
func testScope() MapScope {
	return MapScope{
		"facts": map[string]any{
			"os": map[string]any{"family": "Debian", "name": "Debian"},
		},
		"trusted":     map[string]any{"certname": "web01.example.com"},
		"greeting":    "hello",
		"services":    []any{"ntp", "ssh"},
		"environment": "production",
	}
}

// writeFiles materialises name->content under a fresh temp dir and returns it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// newHiera loads an engine from a fixture tree whose hiera.yaml is at the root.
func newHiera(t *testing.T, scope Scope, files map[string]string) *Hiera {
	t.Helper()
	dir := writeFiles(t, files)
	h, err := Load(filepath.Join(dir, "hiera.yaml"), scope)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return h
}

// primaryFixture is the canonical YAML tree exercising most engine paths.
func primaryFixture() map[string]string {
	return map[string]string{
		"hiera.yaml": `
version: 5
defaults:
  datadir: data
  data_hash: yaml_data
hierarchy:
  - name: "Per-node"
    path: "nodes/%{trusted.certname}.yaml"
  - name: "Per-OS"
    path: "os/%{facts.os.family}.yaml"
  - name: "Common"
    path: "common.yaml"
`,
		"data/common.yaml": `
lookup_options:
  ntp::servers:
    merge: unique
  "^profile::deep::":
    merge:
      strategy: deep
      knockout_prefix: "--"
      sort_merged_arrays: true
users:
  - alice
  - bob
ntp::servers:
  - 0.pool
  - common.pool
greeting: "Hi from %{facts.os.name}"
port: 80
config:
  timeout: 30
  retries: 3
profile::deep::settings:
  a: 1
  list:
    - x
    - y
nested:
  arr:
    - one
    - two
hashval:
  msg: "os is %{facts.os.name}"
whole_alias: "%{alias('users')}"
alias_missing: "%{alias('nope')}"
str_lookup: "servers=%{lookup('ntp::servers')} port=%{lookup('port')}"
bool_lookup: "t=%{lookup('btrue')} f=%{lookup('bfalse')}"
btrue: true
bfalse: false
symval: :asymbol
1: intkeyed
`,
		"data/os/Debian.yaml": `
lookup_options:
  ntp::servers:
    merge: unique
ntp::servers:
  - 1.debian.pool
  - common.pool
config:
  timeout: 10
  extra: true
profile::deep::settings:
  a: 2
  b: 3
  list:
    - "--x"
    - z
`,
		"data/nodes/web01.example.com.yaml": `
port: 8080
`,
	}
}

func mustLookup(t *testing.T, h *Hiera, key string, opts *Options) (Value, bool) {
	t.Helper()
	v, ok, err := h.Lookup(key, opts)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", key, err)
	}
	return v, ok
}

func TestLookupFirst(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	v, ok := mustLookup(t, h, "port", nil)
	if !ok || v != int64(8080) {
		t.Fatalf("port = %v (%T), ok=%v; want 8080", v, v, ok)
	}
}

func TestLookupInterpolationScope(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	v, _ := mustLookup(t, h, "greeting", nil)
	if v != "Hi from Debian" {
		t.Fatalf("greeting = %q", v)
	}
}

func TestLookupUniqueMerge(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	v, _ := mustLookup(t, h, "ntp::servers", nil)
	want := []any{"1.debian.pool", "common.pool", "0.pool"}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("ntp::servers = %#v; want %#v", v, want)
	}
}

func TestLookupHashMergeOverride(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	v, _ := mustLookup(t, h, "config", &Options{Merge: &MergeStrategy{Kind: MergeHash}})
	want := map[string]any{"timeout": int64(10), "extra": true, "retries": int64(3)}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("config = %#v; want %#v", v, want)
	}
}

func TestLookupDeepMergeRegexKnockout(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	v, _ := mustLookup(t, h, "profile::deep::settings", nil)
	want := map[string]any{
		"a":    int64(2),
		"b":    int64(3),
		"list": []any{"y", "z"}, // union [--x,z,x,y] -> knockout x, drop marker, sorted
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("deep = %#v; want %#v", v, want)
	}
}

func TestLookupDig(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	if v, ok := mustLookup(t, h, "nested.arr.1", nil); !ok || v != "two" {
		t.Fatalf("nested.arr.1 = %v ok=%v", v, ok)
	}
	if _, ok := mustLookup(t, h, "nested.arr.9", nil); ok {
		t.Fatal("nested.arr.9 should be out of range")
	}
	if _, ok := mustLookup(t, h, "nested.arr.x", nil); ok {
		t.Fatal("nested.arr.x should not be an integer index")
	}
	if _, ok := mustLookup(t, h, "port.sub", nil); ok {
		t.Fatal("digging into a scalar should fail")
	}
}

func TestLookupWholeAlias(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	v, _ := mustLookup(t, h, "whole_alias", nil)
	if !reflect.DeepEqual(v, []any{"alice", "bob"}) {
		t.Fatalf("whole_alias = %#v", v)
	}
	// alias to a missing key yields nil but found.
	v, ok := mustLookup(t, h, "alias_missing", nil)
	if !ok || v != nil {
		t.Fatalf("alias_missing = %v ok=%v", v, ok)
	}
}

func TestLookupStringAndBoolInterpolation(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	if v, _ := mustLookup(t, h, "str_lookup", nil); v != "servers=[1.debian.pool common.pool 0.pool] port=8080" {
		t.Fatalf("str_lookup = %q", v)
	}
	if v, _ := mustLookup(t, h, "bool_lookup", nil); v != "t=true f=false" {
		t.Fatalf("bool_lookup = %q", v)
	}
}

func TestLookupHashValueInterpolation(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	v, _ := mustLookup(t, h, "hashval", nil)
	if !reflect.DeepEqual(v, map[string]any{"msg": "os is Debian"}) {
		t.Fatalf("hashval = %#v", v)
	}
}

func TestLookupSymbolAndIntKey(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	if v, _ := mustLookup(t, h, "symval", nil); v != "asymbol" {
		t.Fatalf("symval = %v", v)
	}
	if v, _ := mustLookup(t, h, "1", nil); v != "intkeyed" {
		t.Fatalf("int-keyed = %v", v)
	}
}

func TestLookupMissing(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	if _, ok := mustLookup(t, h, "does_not_exist", nil); ok {
		t.Fatal("missing key should not be found")
	}
}

func TestLookupDefaults(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	// default_value with interpolation
	v, ok := mustLookup(t, h, "absent", &Options{HasDefault: true, Default: "user is %{scope('environment')}"})
	if !ok || v != "user is production" {
		t.Fatalf("default = %v ok=%v", v, ok)
	}
	// default_values_hash hit and miss
	dvh := map[string]any{"absent": 42}
	if v, ok := mustLookup(t, h, "absent", &Options{DefaultValuesHash: dvh}); !ok || v != 42 {
		t.Fatalf("dvh hit = %v ok=%v", v, ok)
	}
	if _, ok := mustLookup(t, h, "other", &Options{DefaultValuesHash: dvh}); ok {
		t.Fatal("dvh miss should not be found")
	}
}

func TestLookupLoopDetection(t *testing.T) {
	files := primaryFixture()
	files["data/common.yaml"] += "\nloopy: \"%{lookup('loopy')}\"\naliasloop: \"%{alias('aliasloop')}\"\n"
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("loopy", nil); err == nil {
		t.Fatal("expected lookup loop error")
	}
	if _, _, err := h.Lookup("aliasloop", nil); err == nil {
		t.Fatal("expected alias loop error")
	}
}

func TestLookupRegexLookupOptionCompileError(t *testing.T) {
	// A malformed regex key is skipped, falling back to first-merge.
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": `
lookup_options:
  "^[":
    merge: unique
key:
  - a
`,
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "key", nil); !reflect.DeepEqual(v, []any{"a"}) {
		t.Fatalf("key = %#v", v)
	}
}

func TestLookupOptionsMustBeMapping(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": "lookup_options: not-a-map\nkey: v\n",
	}
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("key", nil); err == nil {
		t.Fatal("expected lookup_options mapping error")
	}
}

func TestInterpolateErrorsPropagate(t *testing.T) {
	cases := map[string]string{
		"unknownfn":    "bad: \"%{nope('a')}\"\n",
		"malformed":    "bad: \"%{scope('a'}\"\n",
		"unterminated": "bad: \"%{scope('a')\"\n",
		"aliaspartial": "bad: \"x %{alias('port')}\"\nport: 1\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{
				"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
				"data/common.yaml": extra,
			}
			h := newHiera(t, testScope(), files)
			if _, _, err := h.Lookup("bad", nil); err == nil {
				t.Fatalf("%s: expected error", name)
			}
		})
	}
}

func TestLiteralAndMissingScope(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": "pct: \"100%{literal('%')}\"\nmiss: \"[%{scope('absent')}]\"\nbare: \"[%{environment}]\"\nhiera_fn: \"%{hiera('port')}\"\nport: 7\n",
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "pct", nil); v != "100%" {
		t.Fatalf("pct = %q", v)
	}
	if v, _ := mustLookup(t, h, "miss", nil); v != "[]" {
		t.Fatalf("miss = %q", v)
	}
	if v, _ := mustLookup(t, h, "bare", nil); v != "[production]" {
		t.Fatalf("bare = %q", v)
	}
	if v, _ := mustLookup(t, h, "hiera_fn", nil); v != "7" {
		t.Fatalf("hiera_fn = %q", v)
	}
}

func TestStructuredInterpolationErrors(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": "arr:\n  - \"%{nope('a')}\"\nhash:\n  k: \"%{nope('a')}\"\n",
	}
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("arr", nil); err == nil {
		t.Fatal("expected array element interpolation error")
	}
	if _, _, err := h.Lookup("hash", nil); err == nil {
		t.Fatal("expected hash value interpolation error")
	}
}

func TestDoubleQuotedArgAndLookupMissing(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": "dq: '%{scope(\"environment\")}'\nmiss: 'x%{lookup(\"nokey\")}y'\n",
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "dq", nil); v != "production" {
		t.Fatalf("dq = %q", v)
	}
	if v, _ := mustLookup(t, h, "miss", nil); v != "xy" {
		t.Fatalf("miss = %q", v)
	}
}

func TestMissingFileSkipped(t *testing.T) {
	// The per-node file does not exist; the lookup falls through to common.
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n" +
			"  - name: node\n    path: \"nodes/%{trusted.certname}.yaml\"\n" +
			"  - name: common\n    path: common.yaml\n",
		"data/common.yaml": "k: fromcommon\n",
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "k", nil); v != "fromcommon" {
		t.Fatalf("k = %v", v)
	}
}

func TestMergeErrorThroughLookup(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": "lookup_options:\n  h:\n    merge: unique\nh:\n  a: 1\n",
	}
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("h", nil); err == nil {
		t.Fatal("expected unique-over-hash merge error")
	}
}

func TestDefaultInterpolationError(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	if _, _, err := h.Lookup("absent", &Options{HasDefault: true, Default: "%{nope('a')}"}); err == nil {
		t.Fatal("expected default interpolation error")
	}
}

func TestGlobInterpolationError(t *testing.T) {
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n  - name: g\n    glob: \"%{alias('x')}/*.yaml\"\n",
	}
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("k", nil); err == nil {
		t.Fatal("expected glob interpolation error")
	}
}

func TestSymbolKey(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": ":skey: sval\n",
	}
	h := newHiera(t, testScope(), files)
	if v, ok := mustLookup(t, h, "skey", nil); !ok || v != "sval" {
		t.Fatalf("skey = %v ok=%v", v, ok)
	}
}

func TestLookupUnknownFnInPathAndDefault(t *testing.T) {
	// lookup() in a path is refused.
	files := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: \"%{lookup('x')}.yaml\"\n",
		"data/common.yaml": "k: v\n",
	}
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("k", nil); err == nil {
		t.Fatal("expected lookup-in-path error")
	}
}
