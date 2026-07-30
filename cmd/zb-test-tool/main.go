// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"iter"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/alecthomas/kong"
	kongcompletion "github.com/jotaen/kong-completion"
	"golang.org/x/tools/txtar"
	"zb.256lights.llc/pkg/internal/aterm"
	"zb.256lights.llc/pkg/internal/storetest"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix/nixbase32"
)

const objectDigestSize = 32

type command struct {
	Debug          bool                   `kong:"help=Show debugging output."`
	GenerateDigest *generateDigestCommand `kong:"cmd"`
	Derivation     *derivationCommand     `kong:"cmd"`
	Txtar          *txtarCommand          `kong:"cmd"`
}

func main() {
	c := new(command)
	k := kong.Must(c,
		kong.Name("zb-test-tool"),
		kong.Description("Utilities for testing zb"),
		kong.Bind(c),
	)
	kongcompletion.Register(k)

	kc, err := k.Parse(os.Args[1:])
	ctx := context.Background()
	initLogging(c.Debug)
	if err != nil {
		log.Errorf(ctx, "%v", err)
		os.Exit(1)
	}
	kc.BindTo(ctx, (*context.Context)(nil))
	err = kc.Run()
	if err != nil {
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
}

type generateDigestCommand struct {
}

func (c *generateDigestCommand) Signature() string {
	return `kong:"cmd,help=Generate a random object digest"`
}

func (c *generateDigestCommand) Run(kc *kong.Context) error {
	buf := make([]byte, 0, objectDigestSize+len("\n"))
	buf = appendNewDigest(buf)
	buf = append(buf, '\n')
	_, err := kc.Stdout.Write(buf)
	return err
}

func appendNewDigest(dst []byte) []byte {
	entropy := make([]byte, nixbase32.DecodedLen(objectDigestSize))
	rand.Read(entropy)
	dst = slices.Grow(dst, objectDigestSize)
	newEnd := len(dst) + objectDigestSize
	nixbase32.Encode(dst[len(dst):newEnd], entropy)
	return dst[:newEnd]
}

type derivationCommand struct {
	InputPlaceholder  *derivationInputPlaceholderCommand  `kong:"cmd"`
	OutputPlaceholder *derivationOutputPlaceholderCommand `kong:"cmd"`
	Format            *derivationFormatCommand            `kong:"cmd,aliases='fmt'"`
}

type derivationInputPlaceholderCommand struct {
	OutputReference zbstore.OutputReference `kong:"arg"`
}

func (c *derivationInputPlaceholderCommand) Signature() string {
	return `kong:"cmd,help=Hash the placeholder for an input derivation\\'s output"`
}

func (c *derivationInputPlaceholderCommand) Run(kc *kong.Context) error {
	_, err := fmt.Fprintln(kc.Stdout, zbstore.UnknownCAOutputPlaceholder(c.OutputReference))
	return err
}

type derivationOutputPlaceholderCommand struct {
	OutputName string `kong:"arg,default=out"`
}

func (c *derivationOutputPlaceholderCommand) Signature() string {
	return `kong:"cmd,help=Hash the placeholder for an output"`
}

func (c *derivationOutputPlaceholderCommand) Run(kc *kong.Context) error {
	_, err := fmt.Fprintln(kc.Stdout, zbstore.HashPlaceholder(c.OutputName))
	return err
}

type derivationFormatCommand struct {
	InputPath string `kong:"name=file,arg,optional"`
	Write     bool   `kong:"short=w,help=Rewrite the input file."`
}

func (c *derivationFormatCommand) Signature() string {
	return `kong:"cmd,help=Pretty-print a derivation with whitespace"`
}

func (c *derivationFormatCommand) Run(kc *kong.Context) error {
	file := os.Stdin
	inputIsStdin := c.InputPath == "" || c.InputPath == "-"
	if !inputIsStdin {
		var flag int
		if c.Write {
			flag = os.O_RDWR
		} else {
			flag = os.O_RDONLY
		}
		var err error
		file, err = os.OpenFile(c.InputPath, flag, 0)
		if err != nil {
			return err
		}
		defer file.Close()
	}

	originalData, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	noWhitespace, err := storetest.MinimizeDerivation(originalData)
	if err != nil {
		return err
	}
	drv := new(zbstore.Derivation)
	if err := drv.UnmarshalText(noWhitespace); err != nil {
		return err
	}

	newData := marshalIndentDerivation(drv)
	if !c.Write || inputIsStdin {
		if _, err := kc.Stdout.Write(newData); err != nil {
			return err
		}
	} else {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := file.Truncate(0); err != nil {
			return err
		}
		if _, err := file.Write(newData); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

type txtarCommand struct {
	FillSystems *txtarFillSystemsCommand `kong:"cmd"`
}

type txtarFillSystemsCommand struct {
	Systems   []system.System `kong:"name=system,default='x86_64-linux,aarch64-linux,aarch64-apple-macos,x86_64-pc-windows'"`
	InputPath string          `kong:"name=file,arg,optional"`
	Write     bool            `kong:"short=w,help=Rewrite the input file."`
}

func (c *txtarFillSystemsCommand) Signature() string {
	return `kong:"cmd,help=Duplicate derivations in a txtar for other systems"`
}

func (c *txtarFillSystemsCommand) Run(kc *kong.Context) error {
	file := os.Stdin
	inputIsStdin := c.InputPath == "" || c.InputPath == "-"
	if !inputIsStdin {
		var flag int
		if c.Write {
			flag = os.O_RDWR
		} else {
			flag = os.O_RDONLY
		}
		var err error
		file, err = os.OpenFile(c.InputPath, flag, 0)
		if err != nil {
			return err
		}
		defer file.Close()
	}

	originalData, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	archive := txtar.Parse(originalData)
	var archiveFileNames iter.Seq[string] = func(yield func(string) bool) {
		for _, file := range archive.Files {
			if !yield(file.Name) {
				return
			}
		}
	}
	for i := 0; i < len(archive.Files); i++ {
		curr := archive.Files[i]
		_, base, _, currSystem, isDrv := splitDerivationFileName(curr.Name)
		if !isDrv {
			continue
		}

		for _, wantSystem := range c.Systems {
			if wantSystem == currSystem {
				continue
			}
			hasWantedSystem := slices.ContainsFunc(archive.Files, func(file txtar.File) bool {
				_, otherBase, _, otherSystem, isDrv := splitDerivationFileName(file.Name)
				if !isDrv {
					return false
				}
				return otherBase == base && otherSystem == wantSystem
			})
			if hasWantedSystem {
				continue
			}

			templateFileIndex := slices.IndexFunc(archive.Files, func(file txtar.File) bool {
				_, otherBase, _, otherSystem, isDrv := splitDerivationFileName(file.Name)
				if !isDrv {
					return false
				}
				return otherBase == base && wantSystem.OS.IsWindows() == otherSystem.OS.IsWindows()
			})
			templateFile := curr
			if templateFileIndex != -1 {
				templateFile = archive.Files[templateFileIndex]
			}

			templateDrvData, err := storetest.MinimizeDerivation(templateFile.Data)
			if err != nil {
				return fmt.Errorf("%s: %v", templateFile.Name, err)
			}
			_, _, templateSystemString, _, _ := splitDerivationFileName(templateFile.Name)
			templateDrvName := joinDerivationName(base, templateSystemString)
			drv := &zbstore.Derivation{Name: templateDrvName}
			if err := drv.UnmarshalText(templateDrvData); err != nil {
				return fmt.Errorf("%s: %v", templateFile.Name, err)
			}
			drv = rewriteDerivationForSystem(drv, wantSystem, archiveFileNames)
			i++
			archive.Files = slices.Insert(archive.Files, i, txtar.File{
				Name: joinDerivationFileName(string(appendNewDigest(nil)), base, wantSystem),
				Data: marshalIndentDerivation(drv),
			})
		}
	}

	newData := txtar.Format(archive)
	if !c.Write || inputIsStdin {
		if _, err := kc.Stdout.Write(newData); err != nil {
			return err
		}
	} else {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := file.Truncate(0); err != nil {
			return err
		}
		if _, err := file.Write(newData); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func rewriteDerivationForSystem(drv *zbstore.Derivation, wantSystem system.System, existingNames iter.Seq[string]) *zbstore.Derivation {
	fakeFileName := strings.Repeat("a", objectDigestSize) + "-" + drv.Name + zbstore.DerivationExt
	_, base, _, originalSystem, ok := splitDerivationFileName(fakeFileName)
	if !ok {
		panic("derivation name bad")
	}

	var wantDirectory zbstore.Directory
	switch {
	case wantSystem.OS.IsWindows() && !originalSystem.OS.IsWindows():
		wantDirectory = zbstore.DefaultWindowsDirectory
	case drv.Dir == "" || !wantSystem.OS.IsWindows() && originalSystem.OS.IsWindows():
		wantDirectory = zbstore.DefaultUnixDirectory
	default:
		wantDirectory = drv.Dir
	}

	var replacements []string
	var newInputSources sets.Sorted[zbstore.Path]
	for oldSrc := range drv.InputSources.Values() {
		newSrc, err := drv.Dir.Object(oldSrc.Base())
		if err != nil {
			panic(err)
		}
		newInputSources.Add(newSrc)
		replacements = append(replacements, string(oldSrc), string(newSrc))
	}

	newInputDerivations := make(map[zbstore.Path]*sets.Sorted[string])
	for oldDrvPath, outputNames := range drv.InputDerivations {
		_, inputBase, _, _, matchesPattern := splitDerivationFileName(oldDrvPath.Base())
		if !matchesPattern {
			newInputDerivations[oldDrvPath] = outputNames
			continue
		}
		var newDrvPath zbstore.Path
		for other := range existingNames {
			_, otherBase, _, otherSystem, otherMatchesPattern := splitDerivationFileName(other)
			if !otherMatchesPattern {
				continue
			}
			if otherBase == inputBase && otherSystem == wantSystem {
				var err error
				newDrvPath, err = wantDirectory.Object(other)
				if err != nil {
					panic(err)
				}
				break
			}
		}
		if newDrvPath == "" {
			newInputDerivations[oldDrvPath] = outputNames
			continue
		}
		newInputDerivations[newDrvPath] = outputNames
		for outputName := range outputNames.Values() {
			oldRef := zbstore.OutputReference{
				DrvPath:    oldDrvPath,
				OutputName: outputName,
			}
			newRef := zbstore.OutputReference{
				DrvPath:    newDrvPath,
				OutputName: outputName,
			}
			replacements = append(replacements,
				zbstore.UnknownCAOutputPlaceholder(oldRef),
				zbstore.UnknownCAOutputPlaceholder(newRef),
			)
		}
	}

	newDrv := drv.ReplaceStrings(strings.NewReplacer(replacements...))
	newDrv.Dir = wantDirectory
	newDrv.Name = joinDerivationName(base, wantSystem.String())
	newDrv.System = wantSystem.String()
	newDrv.InputSources = newInputSources
	newDrv.InputDerivations = newInputDerivations
	return newDrv
}

func splitDerivationFileName(name string) (digest, base, sysString string, sys system.System, ok bool) {
	if len(name) < objectDigestSize+len("-") ||
		nixbase32.ValidateString(name[:objectDigestSize]) != nil ||
		name[objectDigestSize] != '-' {
		return "", "", "", system.System{}, false
	}
	digest = name[:objectDigestSize]
	name = name[objectDigestSize+len("-"):]

	drvName, isDrv := strings.CutSuffix(name, zbstore.DerivationExt)
	if !isDrv || strings.Contains(drvName, "/") {
		return "", "", "", system.System{}, false
	}
	extStart := strings.LastIndexByte(drvName, '.')
	if extStart == -1 {
		extStart = len(drvName)
	}
	base = drvName[:extStart]
	ext := drvName[extStart:]
	for _, arch := range knownArchitectures {
		hyphenIndex := strings.LastIndex(base, "-"+string(arch))
		if hyphenIndex == -1 {
			continue
		}
		sysString = base[hyphenIndex+1:]
		var err error
		sys, err = system.Parse(sysString)
		if err == nil {
			return digest, base[:hyphenIndex] + ext, sysString, sys, true
		}
	}
	return "", "", "", system.System{}, false
}

var knownArchitectures = [...]system.Architecture{
	"x86_64",
	"aarch64",
}

func joinDerivationName(base, sysString string) string {
	extStart := strings.LastIndexByte(base, '.')
	if extStart == -1 {
		extStart = len(base)
	}
	return base[:extStart] + "-" + sysString + base[extStart:]
}

func joinDerivationFileName(digest, base string, sys system.System) string {
	return digest + "-" + joinDerivationName(base, sys.String()) + zbstore.DerivationExt
}

func marshalIndentDerivation(drv *zbstore.Derivation) []byte {
	const indent = "  "
	var buf []byte
	buf = append(buf, "Derive(\n"+indent+"["...)
	if len(drv.Outputs) <= 1 {
		for outName, t := range drv.Outputs {
			var err error
			buf, err = zbstore.AppendDerivationOutput(buf, drv.Dir, drv.Name, outName, t)
			if err != nil {
				panic(err)
			}
		}
	} else {
		buf = append(buf, "\n"+indent...)
		for i, outName := range xmaps.SortedKeys(drv.Outputs) {
			buf = append(buf, indent...)
			var err error
			buf, err = zbstore.AppendDerivationOutput(buf, drv.Dir, drv.Name, outName, drv.Outputs[outName])
			if err != nil {
				panic(err)
			}
			if i < len(drv.Outputs)-1 {
				buf = append(buf, ',')
			}
			buf = append(buf, "\n"+indent...)
		}
	}

	buf = append(buf, "],\n"+indent+"["...)
	if len(drv.InputDerivations) > 0 {
		buf = append(buf, "\n"+indent...)
	}
	for i, k := range xmaps.SortedKeys(drv.InputDerivations) {
		buf = append(buf, indent+"("...)
		buf = aterm.AppendString(buf, string(k))
		buf = append(buf, ", ["...)
		outputs := drv.InputDerivations[k]
		for j, out := range outputs.All() {
			if j > 0 {
				buf = append(buf, ", "...)
			}
			buf = aterm.AppendString(buf, out)
		}
		buf = append(buf, "])"...)
		if i < len(drv.InputDerivations)-1 {
			buf = append(buf, ',')
		}
		buf = append(buf, "\n"+indent...)
	}

	buf = append(buf, "],\n"+indent+"["...)
	if drv.InputSources.Len() > 0 {
		buf = append(buf, "\n"+indent...)
	}
	for i, src := range drv.InputSources.All() {
		buf = append(buf, indent...)
		buf = aterm.AppendString(buf, string(src))
		if i < drv.InputSources.Len()-1 {
			buf = append(buf, ',')
		}
		buf = append(buf, "\n"+indent...)
	}

	buf = append(buf, "],\n"+indent...)
	buf = aterm.AppendString(buf, drv.System)
	buf = append(buf, ",\n"+indent...)
	buf = aterm.AppendString(buf, drv.Builder)

	buf = append(buf, ",\n"+indent+"["...)
	if len(drv.Args) > 0 {
		buf = append(buf, "\n"+indent...)
	}
	for i, arg := range drv.Args {
		buf = append(buf, indent...)
		buf = aterm.AppendString(buf, arg)
		if i < len(drv.Args)-1 {
			buf = append(buf, ',')
		}
		buf = append(buf, "\n"+indent...)
	}

	buf = append(buf, "],\n"+indent+"["...)
	if len(drv.Env) > 0 {
		buf = append(buf, "\n"+indent...)
	}
	for i, k := range xmaps.SortedKeys(drv.Env) {
		buf = append(buf, indent+"("...)
		buf = aterm.AppendString(buf, k)
		buf = append(buf, ", "...)
		buf = aterm.AppendString(buf, drv.Env[k])
		buf = append(buf, ')')
		if i < len(drv.Env)-1 {
			buf = append(buf, ',')
		}
		buf = append(buf, "\n"+indent...)
	}

	buf = append(buf, "]\n)\n"...)

	return buf
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
