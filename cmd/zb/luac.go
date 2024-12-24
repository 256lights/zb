// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/luacode"
)

type luacOptions struct {
	inputFilename  string
	outputFilename string
	list           int
	parseOnly      bool
	stripDebug     bool
	rawPC          bool
}

func newLuacCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "luac FILE",
		Short:                 "luac",
		Args:                  cobra.ExactArgs(1),
		Hidden:                true,
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(luacOptions)
	c.Flags().CountVarP(&opts.list, "list", "l", "produce a listing of compiled bytecode")
	c.Flags().StringVarP(&opts.outputFilename, "output", "o", "luac.out", "output to `filename`")
	c.Flags().BoolVarP(&opts.parseOnly, "parse-only", "p", false, "do not write bytecode")
	c.Flags().BoolVarP(&opts.stripDebug, "strip-debug", "s", false, "strip debug information")
	c.Flags().BoolVarP(&opts.rawPC, "raw-pc", "0", false, "show literal PC values")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.inputFilename = args[0]
		return runLuac(cmd.Context(), g, opts)
	}
	return c
}

func runLuac(ctx context.Context, g *globalConfig, opts *luacOptions) error {
	f, err := os.Open(opts.inputFilename)
	if err != nil {
		return err
	}
	defer f.Close()

	proto, err := luacode.Parse(luacode.FilenameSource(opts.inputFilename), bufio.NewReader(f))
	if err != nil {
		return err
	}
	if opts.list > 0 {
		functionNames := make(map[*luacode.Prototype]string)
		nameFunctions(functionNames, proto)
		pcBase := 0
		if !opts.rawPC {
			pcBase = 1
		}
		if err := printFunction(proto, functionNames, pcBase, opts.list > 1); err != nil {
			return err
		}
	}

	if opts.parseOnly {
		return nil
	}
	output, err := proto.MarshalBinary()
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.outputFilename, output, 0o666); err != nil {
		return err
	}

	return nil
}

func printFunction(f *luacode.Prototype, functionNames map[*luacode.Prototype]string, pcBase int, full bool) error {
	var source string
	if s, ok := f.Source.Abstract(); ok {
		source = s
	} else if s, ok := f.Source.Filename(); ok {
		source = s
	} else if strings.HasPrefix(string(f.Source), luacode.Signature[:1]) {
		source = "(bstring)"
	} else {
		source = "(string)"
	}
	ifElse := func(b bool, t, f string) string {
		if b {
			return t
		} else {
			return f
		}
	}
	plural := func(n int, unit string, unitPlural string) string {
		if n == 1 {
			return "1 " + unit
		}
		return fmt.Sprintf("%d %s", n, unitPlural)
	}
	pluralUnit := func(n int, unit string, unitPlural string) string {
		if n == 1 {
			return unit
		}
		return unitPlural
	}
	_, err := fmt.Printf(
		"\n%s <%s:%d,%d> (%s for %s)\n",
		ifElse(f.IsMainChunk(), "main", "function"),
		source,
		f.LineDefined,
		f.LastLineDefined,
		plural(len(f.Code), "instruction", "instructions"),
		functionNames[f],
	)
	if err != nil {
		return err
	}

	_, err = fmt.Printf(
		"%d%s %s, %s, %s, %s, %s, %s\n",
		f.NumParams,
		ifElse(f.IsVararg, "+", ""),
		pluralUnit(int(f.NumParams), "param", "params"),
		plural(int(f.MaxStackSize), "slot", "slots"),
		plural(len(f.Upvalues), "upvalue", "upvalues"),
		plural(len(f.LocalVariables), "local", "locals"),
		plural(len(f.Constants), "constant", "constants"),
		plural(len(f.Functions), "function", "functions"),
	)
	if err != nil {
		return err
	}

	lineBuf := new(bytes.Buffer)
	for pc, i := range f.Code {
		lineBuf.Reset()
		fmt.Fprintf(lineBuf, "\t%d\t", pcBase+pc)
		if pc < f.LineInfo.Len() {
			line := f.LineInfo.At(pc)
			fmt.Fprintf(lineBuf, "[%d]\t", line)
		} else {
			lineBuf.WriteString("[-]\t")
		}
		lineBuf.WriteString(i.String())

		// Contextual comments.
		switch i.OpCode() {
		case luacode.OpLoadK:
			if bx := i.ArgBx(); int(bx) < len(f.Constants) {
				fmt.Fprintf(lineBuf, "\t; %v", f.Constants[bx])
			}
		case luacode.OpEQK:
			if b := i.ArgB(); int(b) < len(f.Constants) {
				fmt.Fprintf(lineBuf, "\t; %v", f.Constants[b])
			}
		case luacode.OpSetField:
			if b := i.ArgB(); int(b) < len(f.Constants) {
				fmt.Fprintf(lineBuf, "\t; %v", f.Constants[b])
				if c := i.ArgC(); i.K() && int(c) < len(f.Constants) {
					fmt.Fprintf(lineBuf, " %v", f.Constants[c])
				}
			}
		case luacode.OpClosure:
			if bx := i.ArgBx(); int(bx) < len(f.Functions) {
				fmt.Fprintf(lineBuf, "\t; %s", functionNames[f.Functions[bx]])
			}
		case luacode.OpJMP:
			fmt.Fprintf(lineBuf, "\t; to %d", pcBase+pc+1+int(i.J()))
		}

		lineBuf.WriteByte('\n')
		if _, err := os.Stdout.Write(lineBuf.Bytes()); err != nil {
			return err
		}
	}

	if full {
		if _, err := fmt.Printf("constants (%d) for %s\n", len(f.Constants), functionNames[f]); err != nil {
			return err
		}
		for i, k := range f.Constants {
			lineBuf.Reset()
			fmt.Fprintf(lineBuf, "\t%d\t", i)
			switch {
			case k.IsNil():
				lineBuf.WriteString("N")
			case k.IsBoolean():
				lineBuf.WriteString("B")
			case k.IsInteger():
				lineBuf.WriteString("I")
			case k.IsNumber() && !k.IsInteger():
				lineBuf.WriteString("F")
			case k.IsString():
				lineBuf.WriteString("S")
			default:
				lineBuf.WriteString("?")
			}
			lineBuf.WriteString("\t")
			lineBuf.WriteString(k.String())
			lineBuf.WriteByte('\n')
			if _, err := os.Stdout.Write(lineBuf.Bytes()); err != nil {
				return err
			}
		}

		if _, err := fmt.Printf("locals (%d) for %s\n", len(f.LocalVariables), functionNames[f]); err != nil {
			return err
		}
		for i, v := range f.LocalVariables {
			_, err := fmt.Printf(
				"\t%d\t%s\t%d\t%d\n",
				i,
				v.Name,
				pcBase+v.StartPC,
				pcBase+v.EndPC,
			)
			if err != nil {
				return err
			}
		}

		if _, err := fmt.Printf("upvalues (%d) for %s\n", len(f.Upvalues), functionNames[f]); err != nil {
			return err
		}
		for i, uv := range f.Upvalues {
			inStack := "0"
			if uv.InStack {
				inStack = "1"
			}
			_, err := fmt.Printf(
				"\t%d\t%s\t%s\t%d\n",
				i,
				uv.Name,
				inStack,
				uv.Index,
			)
			if err != nil {
				return err
			}
		}
	}

	for _, f := range f.Functions {
		if err := printFunction(f, functionNames, pcBase, full); err != nil {
			return err
		}
	}

	return nil
}

func nameFunctions(names map[*luacode.Prototype]string, f *luacode.Prototype) {
	base := names[f]
	isTop := base == ""
	if isTop {
		if f.IsMainChunk() {
			base = "main"
		} else {
			base = "top"
		}
		names[f] = base
	}

	for i, f := range f.Functions {
		var name string
		if isTop {
			name = fmt.Sprintf("F[%d]", i)
		} else {
			name = fmt.Sprintf("%s[%d]", base, i)
		}
		names[f] = name
		nameFunctions(names, f)
	}
}
