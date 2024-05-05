package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/cgo"
	"sync"

	"github.com/spf13/cobra"
	"zombiezen.com/go/bass/sigterm"
	"zombiezen.com/go/log"
	"zombiezen.com/go/lua"
	"zombiezen.com/go/zb"
)

type globalConfig struct {
	// global options go here
}

func main() {
	rootCommand := &cobra.Command{
		Use:           "zb",
		Short:         "zombiezen build",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	g := new(globalConfig)
	showDebug := rootCommand.PersistentFlags().Bool("debug", false, "show debugging output")
	rootCommand.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		initLogging(*showDebug)
		return nil
	}

	rootCommand.AddCommand(
		newEvalCommand(g),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), sigterm.Signals()...)
	err := rootCommand.ExecuteContext(ctx)
	cancel()
	if err != nil {
		initLogging(*showDebug)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
}

type evalOptions struct {
	expr string
}

func newEvalCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:           "eval",
		Short:         "evaluate a Lua expression",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	opts := new(evalOptions)
	c.Flags().StringVar(&opts.expr, "expr", "", "interpret installables as attribute paths relative to the Lua expression `expr`")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runEval(cmd.Context(), g, opts)
	}
	return c
}

const derivationTypeName = "derivation"

func runEval(ctx context.Context, g *globalConfig, opts *evalOptions) error {
	l := new(lua.State)
	defer l.Close()

	registerDerivationMetatable(l)

	base := lua.NewOpenBase(io.Discard, func(l *lua.State) (int, error) {
		return 0, fmt.Errorf("loadfile not supported")
	})
	if err := lua.Require(l, lua.GName, true, base); err != nil {
		return err
	}
	lua.SetFuncs(l, 0, map[string]lua.Function{
		"derivation": derivationFunction,
	})
	l.Pop(1)

	if err := loadExpression(l, opts.expr); err != nil {
		return err
	}
	if err := l.Call(0, 1, 0); err != nil {
		return err
	}

	s, err := lua.ToString(l, -1)
	if err != nil {
		return err
	}
	fmt.Println(s)

	return nil
}

func registerDerivationMetatable(l *lua.State) {
	lua.NewMetatable(l, derivationTypeName)
	err := lua.SetFuncs(l, 0, map[string]lua.Function{
		"__index":     indexDerivation,
		"__pairs":     derivationPairs,
		"__gc":        gcDerivation,
		"__metatable": nil, // prevent Lua access to metatable
	})
	if err != nil {
		panic(err)
	}
	l.Pop(1)
}

func derivationFunction(l *lua.State) (int, error) {
	if !l.IsTable(1) {
		return 0, lua.NewTypeError(l, 1, lua.TypeTable.String())
	}
	drv := new(zb.Derivation)

	// Start a copy of the table.
	l.CreateTable(0, int(l.RawLen(1)))
	tableCopyIndex := l.Top()

	// Obtain environment variables from extra pairs.
	l.PushNil()
	for l.Next(1) {
		if l.Type(-2) != lua.TypeString {
			// Skip this pair.
			l.Pop(1)
			continue
		}

		// Store copy of pair into table.
		l.PushValue(-2) // Push key.
		l.PushValue(-2) // Push value.
		// TODO(soon): Validate primitive or list.
		l.RawSet(tableCopyIndex)

		// Handle pairs.
		switch k, _ := l.ToString(-2); k {
		case "name":
			if typ := l.Type(-1); typ != lua.TypeString {
				return 0, fmt.Errorf("name argument: %v expected, got %v", lua.TypeString, typ)
			}
			drv.Name, _ = l.ToString(-1)
		case "system":
			if typ := l.Type(-1); typ != lua.TypeString {
				return 0, fmt.Errorf("system argument: %v expected, got %v", lua.TypeString, typ)
			}
			drv.System, _ = l.ToString(-1)
		default:
			v, err := toEnvVar(l, -1)
			if err != nil {
				return 0, fmt.Errorf("%s: %v", k, err)
			}
			if drv.Env == nil {
				drv.Env = make(map[string]string)
			}
			drv.Env[k] = v
		}

		// Remove value, keeping key for the next iteration.
		l.Pop(1)
	}

	l.NewUserdataUV(8, 1)
	l.Rotate(-2, -1) // Swap userdata and argument table copy.
	l.SetUserValue(-2, 1)
	handle := cgo.NewHandle(drv)
	setUserdataHandle(l, -1, handle)

	lua.SetMetatable(l, derivationTypeName)

	return 1, nil
}

func toEnvVar(l *lua.State, idx int) (string, error) {
	switch typ := l.Type(idx); typ {
	case lua.TypeNil:
		return "", nil
	case lua.TypeBoolean:
		if !l.ToBoolean(idx) {
			return "", nil
		}
		return "1", nil
	case lua.TypeString, lua.TypeNumber:
		s, _ := l.ToString(idx)
		return s, nil
	default:
		return "", fmt.Errorf("%v cannot be used as an environment variable", typ)
	}
}

func toDerivation(l *lua.State) (*zb.Derivation, error) {
	const idx = 1
	if _, err := lua.CheckUserdata(l, idx, derivationTypeName); err != nil {
		return nil, err
	}
	drv := testDerivation(l, idx)
	if drv == nil {
		return nil, lua.NewArgError(l, idx, "could not extract derivation")
	}
	return drv, nil
}

func testDerivation(l *lua.State, idx int) *zb.Derivation {
	handle, _ := testUserdataHandle(l, idx, derivationTypeName)
	if handle == 0 {
		return nil
	}
	drv, _ := handle.Value().(*zb.Derivation)
	return drv
}

// gcDerivation handles the __gc metamethod on derivations
// by releasing the [*derivation].
func gcDerivation(l *lua.State) (int, error) {
	const idx = 1
	handle, ok := testUserdataHandle(l, idx, derivationTypeName)
	if !ok {
		return 0, lua.NewTypeError(l, idx, derivationTypeName)
	}
	if handle == 0 {
		return 0, nil
	}
	handle.Delete()
	setUserdataHandle(l, idx, 0)
	return 0, nil
}

// indexDerivation handles the __index metamethod on derivations.
func indexDerivation(l *lua.State) (int, error) {
	if _, err := toDerivation(l); err != nil {
		return 0, err
	}
	l.UserValue(1, 1) // Push derivation argument table.
	l.PushValue(2)    // Copy key argument.
	if _, err := l.Table(-2, 0); err != nil {
		return 0, err
	}
	return 1, nil
}

// derivationPairs handles the __pairs metamethod on derivations.
func derivationPairs(l *lua.State) (int, error) {
	if _, err := toDerivation(l); err != nil {
		return 0, err
	}
	l.UserValue(1, 1) // Push derivation argument table.
	l.PushClosure(1, derivationPairNext)
	l.PushNil()
	l.PushNil()
	return 3, nil
}

// derivationPairNext is the iterator function returned by the derivation __pairs metamethod.
func derivationPairNext(l *lua.State) (int, error) {
	l.PushValue(2) // Move control value to top of stack.
	if !l.Next(lua.UpvalueIndex(1)) {
		l.PushNil()
		return 1, nil
	}
	return 2, nil
}

func testUserdataHandle(l *lua.State, idx int, tname string) (cgo.Handle, bool) {
	data := lua.TestUserdata(l, idx, tname)
	if len(data) != 8 {
		return 0, false
	}
	return cgo.Handle(binary.LittleEndian.Uint64(data)), true
}

func setUserdataHandle(l *lua.State, idx int, handle cgo.Handle) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(handle))
	l.SetUserdata(idx, 0, buf[:])
}

func loadExpression(l *lua.State, expr string) error {
	if err := l.LoadString("return "+expr+";", expr, "t"); err == nil {
		return nil
	}
	l.Pop(1)
	if err := l.LoadString(expr, expr, "t"); err != nil {
		l.Pop(1)
		return err
	}
	return nil
}

var initLogOnce sync.Once

func initLogging(showDebug bool) {
	initLogOnce.Do(func() {
		minLogLevel := log.Info
		if showDebug {
			minLogLevel = log.Debug
		}
		log.SetDefault(&log.LevelFilter{
			Min:    minLogLevel,
			Output: log.New(os.Stderr, "zb: ", log.StdFlags, nil),
		})
	})
}
