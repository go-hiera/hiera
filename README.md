<p align="center"><img src="https://raw.githubusercontent.com/go-hiera/brand/main/social/go-hiera-hiera.png" alt="go-hiera/hiera" width="720"></p>

<!-- The org brand assets live in github.com/go-hiera/brand; the banner above
     renders once that repo is published. -->

# hiera — go-hiera

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-hiera.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of Puppet's [Hiera 5](https://www.puppet.com/docs/puppet/latest/hiera.html)
hierarchical data-lookup engine.** It loads a Hiera 5 configuration
(`hiera.yaml`), walks the configured hierarchy of data sources, and resolves a
key to a typed value — honouring Hiera's merge behaviours, per-key
`lookup_options`, dotted-key digging, and the full `%{...}` interpolation
grammar, with interpolation-loop detection.

It is faithful to Hiera 5 semantics so it can back Puppet's `lookup()` function,
and it is a **standalone, reusable** module: it has **no hard dependency on any
particular fact source**. The caller injects a `Scope` (variable provider), so a
facts engine such as [go-facter](https://github.com/go-facter) — or the Ruby
binding [go-ruby-hiera](https://github.com/go-ruby-hiera) that maps Ruby's
`Hiera` API and Puppet's `lookup()` onto it — plugs its own facts in.

> **What it is — and isn't.** Resolving a hierarchy of YAML/JSON data with merge
> and interpolation is fully deterministic and needs **no Ruby interpreter**, so
> it lives here as pure Go. The Ruby-facing `Hiera`/`lookup()` surface and the
> automatic-data-binding wiring stay in the consumer (go-ruby-hiera / rbgo) —
> this library resolves data, the host presents the Ruby API.

## Features

- **`hiera.yaml` v5 loader** — `version: 5`, `defaults` (datadir + backend
  function), and a `hierarchy` list of levels using `path` / `paths` / `glob` /
  `globs` / `mapped_paths`, with per-level `datadir`, backend and `options`.
- **Backends** — `yaml_data` (via the Ruby-faithful, pure-Go
  [go-ruby-yaml](https://github.com/go-ruby-yaml/yaml) Psych backend) and
  `json_data` (stdlib). Register your own with `RegisterDataHash` — the seam
  eyaml/hocon backends slot into.
- **Merge behaviours** — `first` (default), `unique` (deep-flatten + dedup),
  `hash` (shallow), and `deep` (recursive) with `knockout_prefix`,
  `merge_hash_arrays` and `sort_merged_arrays`.
- **`lookup_options`** — per-key (and `^regex`) merge selection, hash-merged
  across the hierarchy; overridable per call.
- **Dotted-key dig** — `lookup("profile.ntp.servers.0")` looks up the root key,
  merges it, then digs into the structured result (map keys and array indices).
- **Interpolation** — `%{var}`, `%{facts.x}`, `%{trusted.x}`,
  `%{scope('x')}`, `%{lookup('x')}` / `%{hiera('x')}`, `%{alias('x')}`
  (whole-value, type-preserving) and `%{literal('%')}` — resolved against the
  injected `Scope` and recursive lookups, with **interpolation-loop detection**.
- **Defaults** — `default_value` (interpolated) and `default_values_hash`.

CGO-free, **100% test coverage** (including every error branch), `gofmt` +
`go vet` clean, and green across the six 64-bit Go targets (amd64, arm64,
riscv64, loong64, ppc64le, s390x).

## Install

```sh
go get github.com/go-hiera/hiera
```

## Usage

```go
package main

import (
	"fmt"
	"log"

	"github.com/go-hiera/hiera"
)

func main() {
	// The caller supplies the variable/fact scope %{...} resolves against.
	// go-ruby-hiera / go-facter implement hiera.Scope over real facts.
	scope := hiera.MapScope{
		"facts":   map[string]any{"os": map[string]any{"family": "Debian"}},
		"trusted": map[string]any{"certname": "web01.example.com"},
	}

	h, err := hiera.Load("hiera.yaml", scope)
	if err != nil {
		log.Fatal(err)
	}

	// First-found lookup.
	v, found, err := h.Lookup("ntp::servers", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(v, found)

	// Force a merge strategy for this call.
	deep := &hiera.MergeStrategy{Kind: hiera.MergeDeep, KnockoutPrefix: "--"}
	merged, _, _ := h.Lookup("profile::settings", &hiera.Options{Merge: deep})
	fmt.Println(merged)

	// Dotted key: dig into structured data after the merge.
	port, _, _ := h.Lookup("profile.ntp.port", nil)
	fmt.Println(port)
}
```

### The `Scope` seam

```go
// Scope is the pluggable variable provider %{...} resolves against.
type Scope interface {
	Lookup(ref string) (any, bool)
}
```

`ref` is the reference used inside `%{...}` or as the argument to `scope()` — a
bare name (`certname`) or a dotted path (`facts.os.name`). The bundled
`MapScope` digs through nested `map[string]any` / `[]any` values; go-facter and
go-ruby-hiera supply their own implementations backed by live facts.

## Tests & coverage

```sh
go test -race -coverpkg=./... -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # total: 100.0%
```

The suite is fixture-driven: each test builds a throw-away `hiera.yaml` + data
tree under `t.TempDir()` and injects a fake `Scope`, covering every backend,
merge, interpolation and error path.

## License

[BSD-3-Clause](LICENSE) — Copyright (c) 2026, the go-hiera/hiera authors.
