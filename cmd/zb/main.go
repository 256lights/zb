package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/spf13/cobra"
	"zombiezen.com/go/bass/sigterm"
	"zombiezen.com/go/log"
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

func runEval(ctx context.Context, g *globalConfig, opts *evalOptions) error {
	eval := zb.NewEval()
	s, err := eval.Expression(opts.expr)
	if err != nil {
		return err
	}
	fmt.Println(s)
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
