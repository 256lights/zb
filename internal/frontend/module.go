// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/luacode"
	"zb.256lights.llc/pkg/internal/xiter"
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
	var rewrites []string
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

		resp := new(zbstore.RealizeResponse)
		// TODO(someday): Only realize single output.
		// TODO(someday): Batch.
		err = jsonrpc.Do(ctx, eval.store, zbstore.RealizeMethod, resp, &zbstore.RealizeRequest{
			DrvPath: c.outputReference.DrvPath,
		})
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: %v", c.outputReference, err))
			return 2, nil
		}
		output, err := xiter.Single(resp.OutputsByName(c.outputReference.OutputName))
		if err != nil {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: outputs: %v", c.outputReference, err))
			return 2, nil
		}
		if !output.Path.Valid || output.Path.X == "" {
			l.PushNil()
			l.PushString(fmt.Sprintf("realize %v: build failed", c.outputReference))
			return 2, nil
		}
		rewrites = append(rewrites, placeholder, string(output.Path.X))
	}
	if len(rewrites) > 0 {
		filename = strings.NewReplacer(rewrites...).Replace(filename)
	}
	filename, err = absSourcePath(l, filename, filenameContext)
	if err != nil {
		l.PushNil()
		l.PushString(err.Error())
		return 2, nil
	}

	eval.loadedMutex.Lock()
	defer func() {
		eval.loadedState.SetTop(1)
		eval.loadedMutex.Unlock()
	}()

	if got := eval.loadedState.RawField(1, filename); got != lua.TypeNil {
		if err := l.XMove(&eval.loadedState, 1); err != nil {
			return 0, err
		}
		return 1, nil
	}
	eval.loadedState.Pop(1)
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

	eval.importGroup.Add(1)
	go func() {
		// TODO(now): Detect cycles.
		defer func() {
			close(finished)
			eval.importGroup.Done()
		}()
		ctx := eval.baseImportContext
		mod.error = eval.resolveModule(ctx, &mod.state, filename)
		if mod.error != nil {
			mod.state.Close()
		}
	}()

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
	drv, _ := x.(*module)
	return drv
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
