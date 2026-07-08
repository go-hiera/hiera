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

func TestJSONBackend(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":       "version: 5\ndefaults:\n  data_hash: json_data\nhierarchy:\n  - name: c\n    path: common.json\n",
		"data/common.json": `{"jkey": "jval", "arr": [1, 2], "obj": {"n": 3}}`,
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "jkey", nil); v != "jval" {
		t.Fatalf("jkey = %v", v)
	}
	if v, _ := mustLookup(t, h, "obj", nil); !reflect.DeepEqual(v, map[string]any{"n": float64(3)}) {
		t.Fatalf("obj = %#v", v)
	}
}

func TestEntryLevelJSONBackend(t *testing.T) {
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n" +
			"  - name: j\n    path: c.json\n    data_hash: json_data\n",
		"data/c.json": `{"k": "fromjson"}`,
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "k", nil); v != "fromjson" {
		t.Fatalf("k = %v", v)
	}
}

func TestGlobBackend(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":    "version: 5\nhierarchy:\n  - name: g\n    glob: \"g/*.yaml\"\n",
		"data/g/a.yaml": "ga: 1\n",
		"data/g/b.yaml": "gb: 2\n",
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "ga", nil); v != int64(1) {
		t.Fatalf("ga = %v", v)
	}
	if v, _ := mustLookup(t, h, "gb", nil); v != int64(2) {
		t.Fatalf("gb = %v", v)
	}
}

func TestGlobBadPattern(t *testing.T) {
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n  - name: g\n    glob: \"[\"\n",
	}
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("x", nil); err == nil {
		t.Fatal("expected bad glob pattern error")
	}
}

func TestMappedPaths(t *testing.T) {
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n" +
			"  - name: m\n    mapped_paths: [services, svc, \"svc/%{svc}.yaml\"]\n",
		"data/svc/ntp.yaml": "ntp_port: 123\n",
		"data/svc/ssh.yaml": "ssh_port: 22\n",
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "ntp_port", nil); v != int64(123) {
		t.Fatalf("ntp_port = %v", v)
	}
	if v, _ := mustLookup(t, h, "ssh_port", nil); v != int64(22) {
		t.Fatalf("ssh_port = %v", v)
	}
}

func TestMappedPathsMissingOrNonArray(t *testing.T) {
	// scope key absent -> no paths; non-array -> no paths. Both simply yield
	// not-found rather than an error.
	scope := MapScope{"notarray": "scalar"}
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n" +
			"  - name: a\n    mapped_paths: [absent, v, \"x/%{v}.yaml\"]\n" +
			"  - name: b\n    mapped_paths: [notarray, v, \"y/%{v}.yaml\"]\n",
	}
	h := newHiera(t, scope, files)
	if _, ok := mustLookup(t, h, "whatever", nil); ok {
		t.Fatal("expected not found")
	}
}

func TestMappedPathTemplateError(t *testing.T) {
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n" +
			"  - name: m\n    mapped_paths: [services, svc, \"%{alias('svc')}\"]\n",
	}
	h := newHiera(t, testScope(), files)
	if _, _, err := h.Lookup("x", nil); err == nil {
		t.Fatal("expected alias-in-path error")
	}
}

func TestLoadDataReadError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
	})
	// Make the data file a directory so os.ReadFile fails (not IsNotExist).
	if err := os.MkdirAll(filepath.Join(dir, "data", "common.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := Load(filepath.Join(dir, "hiera.yaml"), testScope())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.Lookup("x", nil); err == nil {
		t.Fatal("expected read error")
	}
}

func TestParseErrors(t *testing.T) {
	yamlErr := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": "key:\n\tbad: 1\n", // tab indentation
	}
	h := newHiera(t, testScope(), yamlErr)
	if _, _, err := h.Lookup("x", nil); err == nil {
		t.Fatal("expected yaml parse error")
	}

	seq := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n",
		"data/common.yaml": "- 1\n- 2\n", // top-level sequence
	}
	h = newHiera(t, testScope(), seq)
	if _, _, err := h.Lookup("x", nil); err == nil {
		t.Fatal("expected non-mapping error")
	}

	badJSON := map[string]string{
		"hiera.yaml":  "version: 5\ndefaults:\n  data_hash: json_data\nhierarchy:\n  - name: c\n    path: c.json\n",
		"data/c.json": "{not json",
	}
	h = newHiera(t, testScope(), badJSON)
	if _, _, err := h.Lookup("x", nil); err == nil {
		t.Fatal("expected json parse error")
	}

	jsonSeq := map[string]string{
		"hiera.yaml":  "version: 5\ndefaults:\n  data_hash: json_data\nhierarchy:\n  - name: c\n    path: c.json\n",
		"data/c.json": "[1, 2]",
	}
	h = newHiera(t, testScope(), jsonSeq)
	if _, _, err := h.Lookup("x", nil); err == nil {
		t.Fatal("expected json non-mapping error")
	}
}

func TestBackendResolutionErrors(t *testing.T) {
	unsupported := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n    data_dig: some_dig\n",
		"data/common.yaml": "k: v\n",
	}
	h := newHiera(t, testScope(), unsupported)
	if _, _, err := h.Lookup("k", nil); err == nil {
		t.Fatal("expected unsupported backend-kind error")
	}

	unknown := map[string]string{
		"hiera.yaml":       "version: 5\nhierarchy:\n  - name: c\n    path: common.yaml\n    data_hash: no_such\n",
		"data/common.yaml": "k: v\n",
	}
	h = newHiera(t, testScope(), unknown)
	if _, _, err := h.Lookup("k", nil); err == nil {
		t.Fatal("expected unknown data_hash error")
	}
}

func TestRegisterDataHash(t *testing.T) {
	files := map[string]string{
		"hiera.yaml":    "version: 5\nhierarchy:\n  - name: c\n    path: c.custom\n    data_hash: custom\n",
		"data/c.custom": "ignored",
	}
	h := newHiera(t, testScope(), files)
	h.RegisterDataHash("custom", func(data []byte, path string) (map[string]any, error) {
		return map[string]any{"custom_key": string(data)}, nil
	})
	if v, _ := mustLookup(t, h, "custom_key", nil); v != "ignored" {
		t.Fatalf("custom_key = %v", v)
	}
}

// TestEmptyDataFiles covers the empty-document fast paths of both parsers via a
// glob that matches empty files.
func TestEmptyDataFiles(t *testing.T) {
	if m, err := parseYAML([]byte("  \n"), "x.yaml"); err != nil || len(m) != 0 {
		t.Fatalf("parseYAML empty = %v %v", m, err)
	}
	if m, err := parseJSON([]byte("   "), "x.json"); err != nil || len(m) != 0 {
		t.Fatalf("parseJSON empty = %v %v", m, err)
	}
	if m, err := topHash(nil, "x"); err != nil || len(m) != 0 {
		t.Fatalf("topHash nil = %v %v", m, err)
	}
}
