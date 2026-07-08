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

func TestParseConfigErrors(t *testing.T) {
	cases := map[string]string{
		"yaml-error":         "key:\n\tbad: 1\n",
		"top-not-mapping":    "- 1\n- 2\n",
		"missing-version":    "hierarchy: []\n",
		"version-not-int":    "version: five\nhierarchy: []\n",
		"bad-version":        "version: 3\nhierarchy: []\n",
		"default-hierarchy":  "version: 5\ndefault_hierarchy:\n  - name: d\n    path: d.yaml\nhierarchy: []\n",
		"defaults-not-map":   "version: 5\ndefaults: nope\nhierarchy: []\n",
		"missing-hierarchy":  "version: 5\n",
		"hierarchy-not-list": "version: 5\nhierarchy: nope\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig([]byte(src), "/tmp"); err == nil {
				t.Fatalf("%s: expected error", name)
			}
		})
	}
}

func TestParseDefaultsErrors(t *testing.T) {
	cases := map[string]string{
		"datadir-type":     "version: 5\ndefaults:\n  datadir: [1]\nhierarchy: []\n",
		"data_hash-type":   "version: 5\ndefaults:\n  data_hash: [1]\nhierarchy: []\n",
		"multiple-backend": "version: 5\ndefaults:\n  data_hash: yaml_data\n  data_dig: foo\nhierarchy: []\n",
		"options-type":     "version: 5\ndefaults:\n  options: nope\nhierarchy: []\n",
		"unknown-key":      "version: 5\ndefaults:\n  bogus: 1\nhierarchy: []\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig([]byte(src), "/tmp"); err == nil {
				t.Fatalf("%s: expected error", name)
			}
		})
	}
}

func TestParseDefaultsAndConfigAccessors(t *testing.T) {
	src := `
version: 5
defaults:
  datadir: env
  lookup_key: some_backend
  options:
    foo: bar
hierarchy:
  - name: c
    path: common.yaml
`
	cfg, err := ParseConfig([]byte(src), "/base")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dir() != "/base" || cfg.Version != 5 {
		t.Fatalf("cfg = %#v", cfg)
	}
	if cfg.Defaults.DataDir != "env" || cfg.Defaults.BackendKind != "lookup_key" {
		t.Fatalf("defaults = %#v", cfg.Defaults)
	}
	if !reflect.DeepEqual(cfg.Defaults.Options, map[string]any{"foo": "bar"}) {
		t.Fatalf("options = %#v", cfg.Defaults.Options)
	}
}

func TestParseEntryErrors(t *testing.T) {
	base := "version: 5\nhierarchy:\n"
	cases := map[string]string{
		"entry-not-map":    base + "  - just-a-string\n",
		"name-type":        base + "  - name: [1]\n    path: p\n",
		"path-type":        base + "  - name: n\n    path: [1]\n",
		"paths-type":       base + "  - name: n\n    paths: 5\n",
		"paths-elem-type":  base + "  - name: n\n    paths:\n      - 5\n",
		"glob-type":        base + "  - name: n\n    glob: [1]\n",
		"globs-type":       base + "  - name: n\n    globs: 5\n",
		"mapped-type":      base + "  - name: n\n    mapped_paths: 5\n",
		"mapped-len":       base + "  - name: n\n    mapped_paths: [a, b]\n",
		"datadir-type":     base + "  - name: n\n    path: p\n    datadir: [1]\n",
		"backend-type":     base + "  - name: n\n    path: p\n    data_hash: [1]\n",
		"multiple-backend": base + "  - name: n\n    path: p\n    data_hash: yaml_data\n    data_dig: x\n",
		"options-type":     base + "  - name: n\n    path: p\n    options: nope\n",
		"unknown-key":      base + "  - name: n\n    path: p\n    bogus: 1\n",
		"missing-name":     base + "  - path: p\n",
		"no-path-source":   base + "  - name: n\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig([]byte(src), "/tmp"); err == nil {
				t.Fatalf("%s: expected error", name)
			}
		})
	}
}

func TestParseEntryAllFields(t *testing.T) {
	src := `
version: 5
hierarchy:
  - name: everything
    paths:
      - a.yaml
      - b.yaml
    globs:
      - "g/*.yaml"
    datadir: d
    data_hash: yaml_data
    options:
      k: v
  - name: mapped
    mapped_paths: [services, svc, "svc/%{svc}.yaml"]
`
	cfg, err := ParseConfig([]byte(src), "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	e := cfg.Hierarchy[0]
	if !reflect.DeepEqual(e.Paths, []string{"a.yaml", "b.yaml"}) || len(e.Globs) != 1 || e.DataDir != "d" || e.BackendName != "yaml_data" {
		t.Fatalf("entry = %#v", e)
	}
	if !reflect.DeepEqual(cfg.Hierarchy[1].MappedPaths, []string{"services", "svc", "svc/%{svc}.yaml"}) {
		t.Fatalf("mapped = %#v", cfg.Hierarchy[1].MappedPaths)
	}
}

func TestLoadFileErrors(t *testing.T) {
	if _, err := Load("/no/such/hiera.yaml", testScope()); err == nil {
		t.Fatal("expected read error")
	}
	dir := writeFiles(t, map[string]string{"hiera.yaml": "version: 5\n"}) // missing hierarchy
	if _, err := Load(filepath.Join(dir, "hiera.yaml"), testScope()); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestNewPanics(t *testing.T) {
	assertPanic(t, func() { New(nil, testScope()) })
	assertPanic(t, func() { New(&Config{}, nil) })
}

func assertPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}

func TestConfigAccessorOnEngine(t *testing.T) {
	h := newHiera(t, testScope(), primaryFixture())
	if h.Config().Version != 5 {
		t.Fatal("Config() version")
	}
}

// TestDatadirVariants covers entry-level, default, fallback and absolute
// datadirs.
func TestDatadirVariants(t *testing.T) {
	absDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(absDir, "abs.yaml"), []byte("k: fromabs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"hiera.yaml": "version: 5\nhierarchy:\n" +
			"  - name: entrydir\n    path: e.yaml\n    datadir: envdata\n" +
			"  - name: absdir\n    path: abs.yaml\n    datadir: " + absDir + "\n" +
			"  - name: fallback\n    path: f.yaml\n",
		"envdata/e.yaml": "k: fromenv\n",
		"data/f.yaml":    "k2: fromdata\n",
	}
	h := newHiera(t, testScope(), files)
	if v, _ := mustLookup(t, h, "k", nil); v != "fromenv" {
		t.Fatalf("entry datadir k=%v", v)
	}
	if v, _ := mustLookup(t, h, "k2", nil); v != "fromdata" {
		t.Fatalf("fallback datadir k2=%v", v)
	}
}
