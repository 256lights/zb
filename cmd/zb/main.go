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

	base := lua.NewOpenBase(io.Discard, func(l *lua.State) (int, error) {
		return 0, fmt.Errorf("loadfile not supported")
	})
	if err := lua.Require(l, lua.GName, true, base); err != nil {
		return err
	}
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
