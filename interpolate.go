// Copyright (c) 2026, the go-hiera/hiera authors
//
// SPDX-License-Identifier: BSD-3-Clause

package hiera

import (
	"fmt"
	"regexp"
	"strings"
)

// interpCtx threads the active state of a single lookup: the current variable
// Scope (overlaid for mapped_paths) and the stack of keys currently being
// resolved, used to detect interpolation / lookup loops.
type interpCtx struct {
	scope Scope
	stack []string
}

// enter pushes key onto the resolution stack, refusing a key already present
// (a loop). The returned func pops it.
func (c *interpCtx) enter(key string) (func(), error) {
	for _, k := range c.stack {
		if k == key {
			return nil, fmt.Errorf("hiera: interpolation/lookup loop detected for key %q", key)
		}
	}
	c.stack = append(c.stack, key)
	return func() { c.stack = c.stack[:len(c.stack)-1] }, nil
}

// withScope returns a shallow copy of the context using a different Scope.
func (c *interpCtx) withScope(s Scope) *interpCtx {
	return &interpCtx{scope: s, stack: c.stack}
}

// callRe matches an interpolation function call: name('arg') or name("arg").
var callRe = regexp.MustCompile(`^([a-zA-Z_]\w*)\(\s*(?:'([^']*)'|"([^"]*)")\s*\)$`)

// interpolate resolves every %{...} within a value, recursing through arrays
// and hashes so nested strings are interpolated too.
func (h *Hiera) interpolate(v Value, ctx *interpCtx) (Value, error) {
	switch x := v.(type) {
	case string:
		return h.interpolateStr(x, ctx, false)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			nv, err := h.interpolate(x[i], ctx)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			nv, err := h.interpolate(val, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	default:
		return v, nil
	}
}

// interpolateStr resolves the %{...} tokens in s. When pathMode is true the
// recursive lookup/alias/hiera functions are refused (data-source paths may
// only reference scope variables). A whole-string alias('key') returns the
// aliased value unchanged, preserving its type.
func (h *Hiera) interpolateStr(s string, ctx *interpCtx, pathMode bool) (Value, error) {
	var b strings.Builder
	rest := s
	first := true
	for {
		idx := strings.Index(rest, "%{")
		if idx < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:idx])
		after := rest[idx+2:]
		end := strings.Index(after, "}")
		if end < 0 {
			return nil, fmt.Errorf("hiera: unterminated %%{...} in %q", s)
		}
		expr := strings.TrimSpace(after[:end])
		remaining := after[end+1:]
		val, isAlias, err := h.resolveExpr(expr, ctx, pathMode)
		if err != nil {
			return nil, err
		}
		if isAlias {
			if !first || b.Len() != 0 || remaining != "" {
				return nil, fmt.Errorf("hiera: alias interpolation %q must be the entire value", s)
			}
			return val, nil
		}
		b.WriteString(stringify(val))
		first = false
		rest = remaining
	}
	return b.String(), nil
}

// resolveExpr evaluates one interpolation expression. It returns the resolved
// value, whether it was an alias (a whole-value replacement), and any error.
func (h *Hiera) resolveExpr(expr string, ctx *interpCtx, pathMode bool) (Value, bool, error) {
	if m := callRe.FindStringSubmatch(expr); m != nil {
		fn := m[1]
		arg := m[2]
		if arg == "" {
			arg = m[3]
		}
		switch fn {
		case "literal":
			return arg, false, nil
		case "scope":
			v, _ := ctx.scope.Lookup(arg)
			return v, false, nil
		case "lookup", "hiera":
			if pathMode {
				return nil, false, fmt.Errorf("hiera: %s() is not allowed in a data-source path", fn)
			}
			v, ok, err := h.lookup(arg, nil, ctx)
			if err != nil {
				return nil, false, err
			}
			if !ok {
				return nil, false, nil
			}
			return v, false, nil
		case "alias":
			if pathMode {
				return nil, false, fmt.Errorf("hiera: alias() is not allowed in a data-source path")
			}
			v, _, err := h.lookup(arg, nil, ctx)
			if err != nil {
				return nil, true, err
			}
			return v, true, nil
		default:
			return nil, false, fmt.Errorf("hiera: unknown interpolation function %q", fn)
		}
	}
	if strings.Contains(expr, "(") {
		return nil, false, fmt.Errorf("hiera: malformed interpolation expression %q", expr)
	}
	// Bare variable reference resolved against the scope.
	v, _ := ctx.scope.Lookup(expr)
	return v, false, nil
}

// stringify renders a resolved value for substitution into a surrounding
// string. A missing (nil) value becomes empty, matching Hiera.
func stringify(v Value) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
