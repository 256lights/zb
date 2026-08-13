// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"

	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
)

// OutputMap is an immutable map of names to [*Output] values.
// Names are organized into numbered groups.
// The zero value is an empty map.
type OutputMap struct {
	groups []map[string]*Output
}

// Group returns a map of the outputs in the i'th group,
// where 1 is the first group.
// If i is non-positive or greater than the number of groups in the map,
// then Group returns an empty map.
func (outs OutputMap) Group(i int) OutputMap {
	if i < 1 || i > len(outs.groups) {
		return OutputMap{}
	}
	return OutputMap{groups: outs.groups[i-1 : i]}
}

func (outs OutputMap) groupCount() int {
	n := len(outs.groups)
	for n > 0 && len(outs.groups[n-1]) == 0 {
		n--
	}
	return n
}

// All returns an iterator over the [*Output] values in key order.
func (outs OutputMap) All() iter.Seq2[string, *Output] {
	return func(yield func(string, *Output) bool) {
		if n := outs.groupCount(); n == 0 {
			return
		} else if n == 1 {
			for k, v := range xmaps.Sorted(outs.groups[0]) {
				if !yield(k, v) {
					return
				}
			}
			return
		}

		var keys []string
		keys = appendKeys(keys, outs.groups[0])
		slices.Sort(keys)
		for _, k := range keys {
			v := outs.groups[0][k]
			if k != "" {
				k = "1-" + k
			}
			if !yield(k, v) {
				return
			}
		}
		for i, group := range outs.groups[1:] {
			clear(keys)
			keys = appendKeys(keys[:0], group)
			slices.Sort(keys)

			for _, k := range keys {
				v := group[k]
				if k == "" {
					k = fmt.Sprintf("%d", i+2)
				} else {
					k = fmt.Sprintf("%d-%s", i+2, k)
				}
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

// Get returns the output in the map with the given name or nil if none exists.
func (outs OutputMap) Get(name string) *Output {
	switch n := outs.groupCount(); {
	case n == 0:
		return nil
	case n == 1 || name == "":
		return outs.groups[0][name]
	case !('1' <= name[0] && name[0] <= '9'):
		return nil
	}

	seqEnd := 1
	for ; seqEnd < len(name) && name[seqEnd] != '-'; seqEnd++ {
		if !('0' <= name[seqEnd] && name[seqEnd] <= '9') {
			return nil
		}
	}
	keyStart := seqEnd + 1
	if keyStart > len(name) {
		// Entirely numeric.
		keyStart = len(name)
	} else if keyStart == len(name) {
		// Invalid key: we drop the hyphen if the remaining key is empty.
		return nil
	}

	i, err := strconv.Atoi(name[:seqEnd])
	if err != nil || i < 0 || i >= len(outs.groups) {
		return nil
	}
	return outs.groups[i][name[keyStart:]]
}

// DerivationPaths returns the derivation paths that appear in the i'th group,
// where 1 is the first group.
// If i is non-positive or greater than the number of groups in the map,
// then DerivationPaths returns nil.
func (outs OutputMap) DerivationPaths(i int) []zbstore.Path {
	if i < 1 || i > len(outs.groups) {
		return nil
	}
	group := outs.groups[i-1]
	var n int
	for _, out := range group {
		n += len(out.refs)
	}
	if n == 0 {
		return nil
	}
	drvPaths := make([]zbstore.Path, 0, n)
	for _, out := range xmaps.Sorted(group) {
		for ref := range out.refs {
			if !slices.Contains(drvPaths, ref.DrvPath) {
				drvPaths = append(drvPaths, ref.DrvPath)
			}
		}
	}
	return drvPaths
}

// OutputReferences returns a set of all [zbstore.OutputReference] values that appear in the map.
func (outs OutputMap) OutputReferences() sets.Set[zbstore.OutputReference] {
	result := make(sets.Set[zbstore.OutputReference])
	for _, group := range outs.groups {
		for _, out := range group {
			if out != nil {
				for ref := range out.refs {
					result.Add(ref)
				}
			}
		}
	}
	return result
}

// An Output is a string with dependency information.
// It usually evaluates to a [zbstore.Path].
// The zero value represents an empty string.
type Output struct {
	template string
	paths    sets.Set[zbstore.Path]
	refs     map[zbstore.OutputReference]string
}

func newOutput(s string, sctx sets.Set[string]) (*Output, error) {
	out := &Output{template: s}
	for c := range sctx {
		v, err := parseContextString(c)
		if err != nil {
			return nil, fmt.Errorf("internal error: %v", err)
		}
		switch {
		case v.path != "" && strings.Contains(s, string(v.path)):
			if out.paths == nil {
				out.paths = make(sets.Set[zbstore.Path])
			}
			out.paths.Add(v.path)
		case !v.outputReference.IsZero():
			placeholder := zbstore.UnknownCAOutputPlaceholder(v.outputReference)
			if strings.Contains(s, placeholder) {
				if out.refs == nil {
					out.refs = make(map[zbstore.OutputReference]string)
				}
				out.refs[v.outputReference] = placeholder
			}
		}
	}
	return out, nil
}

// OutputReferences returns a copy of the set of output references
// that the output requires for [*Output.Evaluate].
func (out *Output) OutputReferences() sets.Set[zbstore.OutputReference] {
	if out == nil || len(out.refs) == 0 {
		return nil
	}
	refs := make(sets.Set[zbstore.OutputReference], len(out.refs))
	refs.AddSeq(maps.Keys(out.refs))
	return refs
}

// Evaluate returns the string the [*Output] represents
// and the [zbstore.Path] values the string contains.
// Evaluate returns an error if it does not receive a path for a [zbstore.OutputReference]
// returned from [*Output.OutputReferences].
func (out *Output) Evaluate(m map[zbstore.OutputReference]zbstore.Path) (string, sets.Set[zbstore.Path], error) {
	if out == nil {
		return "", nil, nil
	}
	paths := out.paths.Clone()
	var replacements []string
	for ref, placeholder := range out.refs {
		p, ok := m[ref]
		if !ok {
			return "", nil, fmt.Errorf("evaluate output: missing output for %v", ref)
		}
		replacements = append(replacements, placeholder, string(p))
		paths.Add(p)
	}
	s := strings.NewReplacer(replacements...).Replace(out.template)
	return s, paths, nil
}

// String returns the string the [*Output] represents
// with placeholders replaced with the derivation path and the output name
// inside of substitution brackets.
func (out *Output) String() string {
	if out == nil {
		return ""
	}
	if len(out.refs) == 0 {
		return out.template
	}
	var replacements []string
	for ref, placeholder := range out.refs {
		replacements = append(replacements, placeholder, "⸂"+ref.String()+"⸃")
	}
	return strings.NewReplacer(replacements...).Replace(out.template)
}

// outputsMeta triggers the __outputs event on the value at idx on Lua state l.
// The result of the event is pushed to the top of the stack
// and outputsMeta returns its type.
//
// The metavalue for the __outputs event can be any value.
// If the metavalue is a function, it is called with the value and system as arguments,
// and the result of the call (adjusted to one value) is the result of the operation.
// If the metavalue is nil, the result of the operation is the value.
// Otherwise, outputsMeta applies the rules above recursively to the metavalue.
func outputsMeta(ctx context.Context, l *lua.State, idx int, sys string) (lua.Type, error) {
	if !l.CheckStack(3) {
		return lua.TypeNil, fmt.Errorf("%s'__outputs': stack overflow", lua.Where(l, 1))
	}
	l.PushValue(idx)
	for range 200 {
		switch lua.Metafield(l, -1, "__outputs") {
		case lua.TypeFunction:
			l.Insert(-2)
			l.PushString(sys)
			if err := l.Call(ctx, 2, 1); err != nil {
				return lua.TypeNil, err
			}
			fallthrough
		case lua.TypeNil:
			return l.Type(-1), nil
		default:
			l.Remove(-2)
		}
	}
	l.Pop(1)
	return lua.TypeNil, fmt.Errorf("%s'__outputs' chain too long; possible loop", lua.Where(l, 1))
}

// objectOutputs gets the output group for the value on the index
// by triggering the __outputs event.
func objectOutputs(ctx context.Context, l *lua.State, idx int, sys system.System) (map[string]*Output, error) {
	defer l.SetTop(l.Top())

	tp, err := outputsMeta(ctx, l, idx, SystemTriple(sys))
	if err != nil {
		return nil, err
	}

	switch tp {
	case lua.TypeNil:
		return nil, nil
	case lua.TypeString, lua.TypeNumber:
		s, _ := l.ToString(-1)
		out, err := newOutput(s, l.StringContext(-1))
		if err != nil {
			return nil, err
		}
		return map[string]*Output{"": out}, nil
	}

	if lua.Metafield(l, -1, "__pairs") != lua.TypeNil {
		l.Insert(-2)
		if err := l.Call(ctx, 1, 3); err != nil {
			return nil, err
		}
	} else if lua.Metafield(l, -1, "__tostring") != lua.TypeNil {
		l.Insert(-2)
		if err := l.Call(ctx, 1, 1); err != nil {
			return nil, err
		}
		if !l.IsString(-1) {
			return nil, fmt.Errorf("lua: '__tostring' must return a string")
		}
		s, _ := l.ToString(-1)
		out, err := newOutput(s, l.StringContext(-1))
		if err != nil {
			return nil, err
		}
		return map[string]*Output{"": out}, nil
	} else {
		l.PushPureFunction(0, baseNext)
		l.Rotate(-2, -1)
		l.PushNil()
	}

	result := make(map[string]*Output)
	for {
		l.PushValue(-3)  // iterator function
		l.PushValue(-3)  // state
		l.Rotate(-3, -1) // move control variable to top
		if err := l.Call(ctx, 2, 2); err != nil {
			return nil, err
		}
		if l.IsNil(-2) {
			if result[""] == nil {
				for _, name := range [...]string{"1", zbstore.DefaultDerivationOutputName} {
					if out := result[name]; out != nil {
						result[""] = out
						delete(result, name)
						break
					}
				}
			}
			return result, nil
		}

		l.PushValue(-2) // Copy key to avoid mutation.
		key, ok := l.ToString(-1)
		if !ok {
			l.Pop(2) // Pop value and key copy.
			continue
		}
		l.Pop(1) // Pop key copy.
		s, sctx, err := lua.ToString(ctx, l, -1)
		if err != nil {
			return nil, err
		}
		out, err := newOutput(s, sctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", key, err)
		}
		result[key] = out
		l.Pop(1) // Pop value.
	}
}

// SystemTriple returns the string passed to the __outputs metamethod for a given system.
func SystemTriple(sys system.System) string {
	result := sys.Arch.String() + "-" + sys.Vendor.String() + "-" + sys.OS.String()
	if !sys.Env.IsUnknown() && !(sys.Env == "msvc" && sys.OS.IsWindows()) {
		result += "-" + sys.Env.String()
	}
	return result
}

func baseNext(ctx context.Context, l *lua.State) (int, error) {
	if got, want := l.Type(1), lua.TypeTable; got != want {
		return 0, lua.NewTypeError(l, 1, want.String())
	}
	l.SetTop(2)
	if !l.Next(1) {
		l.PushNil()
		return 1, nil
	}
	return 2, nil
}

func appendKeys[K comparable, V any, Map ~map[K]V, Slice ~[]K](dst Slice, m Map) Slice {
	dst = slices.Grow(dst, len(m))
	return slices.AppendSeq(dst, maps.Keys(m))
}
