// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"
	"sync"

	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
)

const moduleTypeName = "module"

// A module is a promise returned from the global import function
// for a loaded immutable value.
type module struct {
	// finished is closed when the module's execution has finished.
	finished <-chan struct{}
	// error is the execution error raised during execution, if any.
	error error

	// mu is held to XMove from state.
	mu sync.Mutex
	// state is the module's execution environment.
	// Once the module has finished execution,
	// its return value is the only value left on the state's stack.
	state lua.State
}

func (mod *module) Freeze() error { return nil }

func registerModuleMetatable(ctx context.Context, l *lua.State) error {
	lua.NewMetatable(l, moduleTypeName)
	funcs := map[string]lua.Function{
		"__index":     indexModule,
		"__concat":    concatModule,
		"__len":       moduleLen,
		"__call":      callModule,
		"__tostring":  moduleToString,
		"__pairs":     modulePairs,
		"__metatable": nil, // prevent Lua access to metatable
	}
	for op := range luacode.AllArithmeticOperators() {
		funcs[op.TagMethod().String()] = func(ctx context.Context, l *lua.State) (int, error) {
			return moduleArithmetic(ctx, l, op)
		}
	}
	for op := range lua.AllComparisonOperators() {
		funcs[op.TagMethod().String()] = func(ctx context.Context, l *lua.State) (int, error) {
			return moduleCompare(ctx, l, op)
		}
	}
	if err := lua.SetPureFunctions(ctx, l, 0, funcs); err != nil {
		return err
	}
	l.Pop(1)
	return nil
}

// importFunction is the global import function implementation.
func (eval *Eval) importFunction(ctx context.Context, l *lua.State) (int, error) {
	filename, err := lua.CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	filenameContext := l.StringContext(1)

	// TODO(someday): If we have dependencies and we're using a non-local store,
	// export the store object and read it.
	toRealize := make(sets.Set[zbstore.OutputReference])
	placeholders := make(map[string]zbstore.OutputReference)
	for dep := range filenameContext {
		c, err := parseContextString(dep)
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("internal error: %v", err))
			return 2, nil
		}
		if c.outputReference.IsZero() {
			continue
		}
		placeholder := zbstore.UnknownCAOutputPlaceholder(c.outputReference)
		if !strings.Contains(filename, placeholder) {
			continue
		}
		toRealize.Add(c.outputReference)
		placeholders[placeholder] = c.outputReference
	}
	if toRealize.Len() > 0 {
		results, err := eval.store.Realize(ctx, toRealize)
		if err != nil {
			l.PushNil()
			l.PushString(err.Error())
			return 2, nil
		}

		var rewrites []string
		for placeholder, outputReference := range placeholders {
			outputPath, err := zbstore.FindRealizeOutput(slices.Values(results), outputReference)
			if err != nil {
				l.PushNil()
				l.PushString(err.Error())
				return 2, nil
			}
			if !outputPath.Valid || outputPath.X == "" {
				l.PushNil()
				l.PushString(fmt.Sprintf("realize %v: build failed", outputReference))
				return 2, nil
			}
			rewrites = append(rewrites, placeholder, string(outputPath.X))
		}
		filename = strings.NewReplacer(rewrites...).Replace(filename)
	}

	filename, err = absSourcePath(l, filename, filenameContext)
	if err != nil {
		l.PushNil()
		l.PushString(err.Error())
		return 2, nil
	}

	// Return error if there's an import cycle.
	chain := importChainFromContext(ctx)
	if chain.has(filename) {
		var list []string
		for chainPath := range chain.All() {
			if chainPath == filename {
				break
			}
			list = append(list, chainPath)
		}
		slices.Reverse(list)

		sb := new(strings.Builder)
		sb.WriteString("import cycle: ")
		sb.WriteString(filename)
		for _, chainPath := range list {
			sb.WriteString("\n→ ")
			sb.WriteString(chainPath)
		}
		l.PushNil()
		l.PushString(sb.String())
		return 2, nil
	}

	// Begin critical section on loaded state.
	eval.loadedMutex.Lock()
	defer func() {
		eval.loadedState.SetTop(1)
		eval.loadedMutex.Unlock()
	}()

	// See if the module has already been imported.
	if got := eval.loadedState.RawField(1, filename); got != lua.TypeNil {
		if err := l.XMove(&eval.loadedState, 1); err != nil {
			return 0, err
		}
		return 1, nil
	}
	eval.loadedState.Pop(1)

	// Create new module instance.
	finished := make(chan struct{})
	mod := &module{finished: finished}
	if err := eval.initState(&mod.state); err != nil {
		return 0, err
	}
	eval.loadedState.NewUserdata(mod, 0)
	if err := lua.SetMetatable(&eval.loadedState, moduleTypeName); err != nil {
		return 0, err
	}
	if err := eval.loadedState.Freeze(-1); err != nil {
		return 0, err
	}
	eval.loadedState.PushValue(-1)
	if err := eval.loadedState.RawSetField(1, filename); err != nil {
		return 0, err
	}

	// Start a goroutine that evaluates the module file.
	eval.importGroup.Add(1)
	go func() {
		defer func() {
			close(finished)
			eval.importGroup.Done()
		}()
		ctx := contextWithImportChain(eval.baseImportContext, &importChain{
			path: filename,
			next: chain,
		})
		mod.error = eval.resolveModule(ctx, &mod.state, filename)
		if mod.error != nil {
			mod.state.Close()
		}
	}()

	// Copy module from loaded state to top of stack.
	if err := l.XMove(&eval.loadedState, 1); err != nil {
		return 0, err
	}
	return 1, nil
}

func (eval *Eval) resolveModule(ctx context.Context, l *lua.State, filename string) error {
	l.SetTop(0)
	if err := loadFile(l, filename); err != nil {
		return err
	}
	if err := l.Call(ctx, 0, lua.MultipleReturns); err != nil {
		return err
	}
	if l.Top() >= 1 {
		// If the file returned at least one value,
		// use that directly.
		l.SetTop(1)
	} else {
		// Push _G. Presumably there were _ENV.foo assignments.
		if _, err := l.Index(ctx, lua.RegistryIndex, lua.RegistryIndexGlobals); err != nil {
			return err
		}
	}
	if err := l.Freeze(1); err != nil {
		return err
	}
	return nil
}

func indexModule(ctx context.Context, l *lua.State) (int, error) {
	l.SetTop(2)
	if err := waitForModuleArg(ctx, l); err != nil {
		return 0, err
	}
	l.Insert(2)
	if _, err := l.Table(ctx, 2); err != nil {
		return 0, err
	}
	return 1, nil
}

func modulePairs(ctx context.Context, l *lua.State) (int, error) {
	if err := waitForModuleArg(ctx, l); err != nil {
		return 0, err
	}
	if lua.Metafield(l, -1, "__pairs") != lua.TypeNil {
		l.Insert(-2) // Move before module value.
		if err := l.Call(ctx, 1, 3); err != nil {
			return 0, err
		}
		return 3, nil
	}

	// Insert _G.next before module value.
	if _, err := l.Field(ctx, lua.RegistryIndex, stdlibRegistryKey); err != nil {
		return 0, err
	}
	if _, err := l.Field(ctx, -1, "next"); err != nil {
		return 0, err
	}
	l.Insert(-3)
	l.Pop(1)

	l.PushNil()
	return 3, nil
}

func moduleToString(ctx context.Context, l *lua.State) (int, error) {
	if err := waitForModuleArg(ctx, l); err != nil {
		return 0, err
	}
	s, sctx, err := lua.ToString(ctx, l, -1)
	if err != nil {
		return 0, err
	}
	l.PushStringContext(s, sctx)
	return 1, nil
}

func concatModule(ctx context.Context, l *lua.State) (int, error) {
	if mod := testModule(l, 1); mod == nil {
		l.PushValue(1)
	} else if err := waitForModule(ctx, l, mod); err != nil {
		return 0, err
	}
	if mod := testModule(l, 2); mod == nil {
		l.PushValue(2)
	} else if err := waitForModule(ctx, l, mod); err != nil {
		return 0, err
	}
	if err := l.Concat(ctx, 2); err != nil {
		return 0, err
	}
	return 1, nil
}

func callModule(ctx context.Context, l *lua.State) (int, error) {
	if err := waitForModuleArg(ctx, l); err != nil {
		return 0, err
	}
	l.Replace(1)
	if err := l.Call(ctx, l.Top()-1, lua.MultipleReturns); err != nil {
		return 0, err
	}
	return l.Top(), nil
}

func moduleLen(ctx context.Context, l *lua.State) (int, error) {
	if err := waitForModuleArg(ctx, l); err != nil {
		return 0, err
	}
	if err := l.Len(ctx, -1); err != nil {
		return 0, err
	}
	return 1, nil
}

func moduleCompare(ctx context.Context, l *lua.State, op lua.ComparisonOperator) (int, error) {
	if mod := testModule(l, 1); mod == nil {
		l.PushValue(1)
	} else if err := waitForModule(ctx, l, mod); err != nil {
		return 0, err
	}
	if mod := testModule(l, 2); mod == nil {
		l.PushValue(2)
	} else if err := waitForModule(ctx, l, mod); err != nil {
		return 0, err
	}
	result, err := l.Compare(ctx, -2, -1, op)
	if err != nil {
		return 0, err
	}
	l.PushBoolean(result)
	return 1, nil
}

func moduleArithmetic(ctx context.Context, l *lua.State, op luacode.ArithmeticOperator) (int, error) {
	if mod := testModule(l, 1); mod == nil {
		l.PushValue(1)
	} else if err := waitForModule(ctx, l, mod); err != nil {
		return 0, err
	}
	if mod := testModule(l, 2); mod == nil {
		l.PushValue(2)
	} else if err := waitForModule(ctx, l, mod); err != nil {
		return 0, err
	}
	if err := l.Arithmetic(ctx, op); err != nil {
		return 0, err
	}
	return 1, nil
}

func awaitFunction(ctx context.Context, l *lua.State) (int, error) {
	l.PushValue(1)
	for {
		mod := testModule(l, -1)
		if mod == nil {
			break
		}
		l.Pop(1)
		if err := waitForModule(ctx, l, mod); err != nil {
			return 0, err
		}
	}
	return 1, nil
}

// waitForModuleArg verifies that the first argument of the current [lua.Function]
// is a [*module] userdata
// and pushes its return value onto l's stack.
func waitForModuleArg(ctx context.Context, l *lua.State) error {
	const idx = 1
	if _, err := lua.CheckUserdata(l, idx, moduleTypeName); err != nil {
		return err
	}
	mod := testModule(l, idx)
	if mod == nil {
		return lua.NewArgError(l, idx, "could not extract module")
	}
	if err := waitForModule(ctx, l, mod); err != nil {
		return err
	}
	return nil
}

// testModule returns the [*module] at the given index of l's stack
// or nil if the value at the given index is not a module userdata.
func testModule(l *lua.State, idx int) *module {
	x, _ := lua.TestUserdata(l, idx, moduleTypeName)
	mod, _ := x.(*module)
	return mod
}

// waitForModule waits for the module to load and pushes its return value onto l's stack.
func waitForModule(ctx context.Context, l *lua.State, mod *module) error {
	select {
	case <-mod.finished:
	case <-ctx.Done():
		return ctx.Err()
	}
	if mod.error != nil {
		return mod.error
	}

	mod.mu.Lock()
	mod.state.PushValue(-1)
	err := l.XMove(&mod.state, 1)
	mod.mu.Unlock()

	return err
}

type importChain struct {
	path string
	next *importChain
}

func importChainFromContext(ctx context.Context) *importChain {
	chain, _ := ctx.Value(importChainContextKey{}).(*importChain)
	return chain
}

func contextWithImportChain(parent context.Context, chain *importChain) context.Context {
	return context.WithValue(parent, importChainContextKey{}, chain)
}

func (chain *importChain) has(path string) bool {
	for chainPath := range chain.All() {
		if chainPath == path {
			return true
		}
	}
	return false
}

func (chain *importChain) All() iter.Seq[string] {
	return func(yield func(string) bool) {
		for curr := chain; curr != nil; curr = curr.next {
			if !yield(curr.path) {
				return
			}
		}
	}
}

type importChainContextKey struct{}
