package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"

	"github.com/spf13/cobra"
	"zombiezen.com/go/bass/sigterm"
	"zombiezen.com/go/log"
	"zombiezen.com/go/lua"
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

func runEval(ctx context.Context, g *globalConfig, opts *evalOptions) error {
	l := new(lua.State)
	defer l.Close()

	// TODO(soon): lua.NewMetatable(l, "derivation")

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

type derivation struct {
	name    string
	system  string
	builder string
	args    []string
	env     map[string]string
}

func derivationFunction(l *lua.State) (int, error) {
	if !l.IsTable(1) {
		return 0, lua.NewTypeError(l, 1, lua.TypeTable.String())
	}
	drv := new(derivation)

	if typ, err := l.Field(1, "name", 0); err != nil {
		return 0, fmt.Errorf("name argument: %v", err)
	} else if typ != lua.TypeString {
		return 0, fmt.Errorf("name argument: %v expected, got %v", lua.TypeString, typ)
	}
	drv.name, _ = l.ToString(-1)
	l.Pop(1)

	if typ, err := l.Field(1, "system", 0); err != nil {
		return 0, fmt.Errorf("system argument: %v", err)
	} else if typ != lua.TypeString {
		return 0, fmt.Errorf("system argument: %v expected, got %v", lua.TypeString, typ)
	}
	drv.system, _ = l.ToString(-1)
	l.Pop(1)

	// Obtain environment variables from extra pairs.
	l.PushNil()
	reserved := map[string]struct{}{
		"name":   {},
		"system": {},
	}
	for l.Next(1) {
		// We need to be careful not to use state.ToString on the key
		// without checking its type first,
		// since state.ToString may change the value on the stack.
		// We clone the value here to be safe.
		l.PushValue(-2)
		k, _ := lua.ToString(l, -1)
		l.Pop(1)

		if _, known := reserved[k]; !known {
			v, err := toEnvVar(l, -1)
			if err != nil {
				return 0, fmt.Errorf("%s: %v", k, err)
			}
			if drv.env == nil {
				drv.env = make(map[string]string)
			}
			drv.env[k] = v
		}

		// Remove value, keeping key for the next iteration.
		l.Pop(1)
	}

	// TODO(soon): Return a Lua value.
	fmt.Printf("%#v\n", drv)
	return 0, nil
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
