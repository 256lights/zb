// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package luac provides a Kong command for a Lua compiler.
// Its command-line options and behavior are roughly the same as [luac(1)].
//
// [luac(1)]: https://www.lua.org/manual/5.4/luac.html
package luac

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/luacode"
)

// Command is a [github.com/alecthomas/kong] command for luac.
type Command struct {
	InputFilename  string `kong:"name=file,arg,required,help=Lua file to parse."`
	Source         string `kong:"placeholder=name,help=Source name to show in debug information instead of filename."`
	OutputFilename string `kong:"name=output,short=o,placeholder=filename,help=Output to file. (default ${default}),default=luac.out"`
	List           int    `kong:"short=l,type=counter,help=Produce a listing of compiled bytecode."`
	ParseOnly      bool   `kong:"short=p,help=Do not write bytecode."`
	StripDebug     bool   `kong:"short=s,help=Strip debug information."`
	RawPC          bool   `kong:"short=0,help=Show literal PC values."`
}

// New returns a new luac command.
func New() *cobra.Command {
	cc := &cobra.Command{
		Use:                   "luac FILE",
		Short:                 "luac",
		Args:                  cobra.ExactArgs(1),
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c := new(Command)
	cc.Flags().CountVarP(&c.List, "list", "l", "produce a listing of compiled bytecode")
	cc.Flags().StringVarP(&c.OutputFilename, "output", "o", "luac.out", "output to `filename`")
	cc.Flags().BoolVarP(&c.ParseOnly, "parse-only", "p", false, "do not write bytecode")
	cc.Flags().BoolVarP(&c.StripDebug, "strip-debug", "s", false, "strip debug information")
	cc.Flags().BoolVarP(&c.RawPC, "raw-pc", "0", false, "show literal PC values")
	cc.Flags().StringVar(&c.Source, "source", "", "source `name` to show in debug information instead of filename")
	cc.RunE = func(cmd *cobra.Command, args []string) error {
		c.InputFilename = args[0]
		return c.Run()
	}
	return cc
}

// Run runs luac.
func (c *Command) Run() error {
	f, err := os.Open(c.InputFilename)
	if err != nil {
		return err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	var proto *luacode.Prototype
	if header, _ := br.Peek(len(luacode.Signature)); string(header) == luacode.Signature {
		bytecode, err := io.ReadAll(br)
		if err != nil {
			return err
		}
		proto = new(luacode.Prototype)
		if err := proto.UnmarshalBinary(bytecode); err != nil {
			return err
		}
	} else {
		var sourceName luacode.Source
		if c.Source != "" {
			sourceName = luacode.Source(c.Source)
		} else {
			sourceName = luacode.FilenameSource(c.InputFilename)
		}
		var err error
		proto, err = luacode.Parse(sourceName, br)
		if err != nil {
			return err
		}
	}

	if c.List > 0 {
		functionNames := make(map[*luacode.Prototype]string)
		nameFunctions(functionNames, proto)
		pcBase := 0
		if !c.RawPC {
			pcBase = 1
		}
		if err := printFunction(proto, functionNames, pcBase, c.List > 1); err != nil {
			return err
		}
	}

	if c.ParseOnly {
		return nil
	}
	output, err := proto.MarshalBinary()
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.OutputFilename, output, 0o666); err != nil {
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
		case luacode.OpGetField:
			if c := i.ArgC(); int(c) < len(f.Constants) {
				fmt.Fprintf(lineBuf, "\t; %v", f.Constants[c])
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
