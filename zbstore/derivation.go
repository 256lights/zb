// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zbstore

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strings"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nixbase32"
	"zombiezen.com/go/zb/internal/aterm"
	"zombiezen.com/go/zb/internal/sortedset"
)

// DerivationExt is the file extension for a marshalled [Derivation].
const DerivationExt = ".drv"

// A Derivation represents a store derivation:
// a single, specific, constant build action.
type Derivation struct {
	// Dir is the store directory this derivation is a part of.
	Dir Directory

	// Name is the human-readable name of the derivation,
	// i.e. the part after the digest in the store object name.
	Name string
	// System is a string representing the OS and architecture tuple
	// that this derivation is intended to run on.
	System string
	// Builder is the path to the program to run the build.
	Builder string
	// Args is the list of arguments that should be passed to the builder program.
	Args []string
	// Env is the environment variables that should be passed to the builder program.
	Env map[string]string

	// InputSources is the set of source filesystem objects that this derivation depends on.
	InputSources sortedset.Set[Path]
	// InputDerivations is the set of derivations that this derivation depends on.
	// The mapped values are the set of output names that are used.
	InputDerivations map[Path]*sortedset.Set[string]
	// Outputs is the set of outputs that the derivation produces.
	Outputs map[string]*DerivationOutput
}

// ParseDerivation parses a derivation from ATerm format.
func ParseDerivation(dir Directory, name string, data []byte) (*Derivation, error) {
	drv := &Derivation{
		Dir:  dir,
		Name: name,
	}
	if err := drv.unmarshalText(data); err != nil {
		return nil, err
	}
	return drv, nil
}

// Export marshals the derivation in ATerm format
// and computes the derivation's store path using the given hashing algorithm.
//
// At the moment, the only supported algorithm is [nix.SHA256].
func (drv *Derivation) Export(hashType nix.HashType) (Path, []byte, error) {
	if drv.Name == "" {
		return "", nil, fmt.Errorf("export derivation: missing name")
	}
	if drv.Dir == "" {
		return "", nil, fmt.Errorf("export %s derivation: missing store directory", drv.Name)
	}

	data, err := drv.marshalText(false)
	if err != nil {
		return "", nil, err
	}
	h := nix.NewHasher(hashType)
	h.Write(data)

	p, err := FixedCAOutputPath(
		drv.Dir,
		drv.Name+DerivationExt,
		nix.TextContentAddress(h.SumHash()),
		drv.References(),
	)
	if err != nil {
		return "", data, err
	}
	return p, data, nil
}

// References returns the set of other store paths that the derivation references.
func (drv *Derivation) References() References {
	refs := References{}
	refs.Others.Grow(drv.InputSources.Len() + len(drv.InputDerivations))
	refs.Others.AddSet(&drv.InputSources)
	for input := range drv.InputDerivations {
		refs.Others.Add(input)
	}
	return refs
}

// MarshalText converts the derivation to ATerm format.
func (drv *Derivation) MarshalText() ([]byte, error) {
	return drv.marshalText(false)
}

func (drv *Derivation) marshalText(maskOutputs bool) ([]byte, error) {
	if drv.Name == "" {
		return nil, fmt.Errorf("marshal derivation: missing name")
	}
	if drv.Dir == "" {
		return nil, fmt.Errorf("marshal %s derivation: missing store directory", drv.Name)
	}

	var buf []byte
	buf = append(buf, "Derive(["...)
	for i, outName := range sortedKeys(drv.Outputs) {
		if i > 0 {
			buf = append(buf, ',')
		}
		var err error
		buf, err = drv.Outputs[outName].marshalText(buf, drv.Dir, drv.Name, outName, maskOutputs)
		if err != nil {
			return nil, fmt.Errorf("marshal %s derivation: %v", drv.Name, err)
		}
	}

	buf = append(buf, "],["...)
	for i, drvPath := range sortedKeys(drv.InputDerivations) {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '(')
		if got := drvPath.Dir(); got != drv.Dir {
			return nil, fmt.Errorf("marshal %s derivation: inputs: unexpected store directory %s (using %s)",
				drv.Name, got, drv.Dir)
		}
		buf = aterm.AppendString(buf, string(drvPath))
		buf = append(buf, ",["...)
		// TODO(someday): This can be some kind of tree? See DerivedPathMap.
		outputs := drv.InputDerivations[drvPath]
		for j := 0; j < outputs.Len(); j++ {
			if j > 0 {
				buf = append(buf, ',')
			}
			buf = aterm.AppendString(buf, outputs.At(j))
		}
		buf = append(buf, "])"...)
	}

	buf = append(buf, "],["...)
	for i := 0; i < drv.InputSources.Len(); i++ {
		src := drv.InputSources.At(i)
		if i > 0 {
			buf = append(buf, ',')
		}
		if got := src.Dir(); got != drv.Dir {
			return nil, fmt.Errorf("marshal %s derivation: inputs: unexpected store directory %s (using %s)",
				drv.Name, got, drv.Dir)
		}
		buf = aterm.AppendString(buf, string(src))
	}

	buf = append(buf, "],"...)
	buf = aterm.AppendString(buf, drv.System)
	buf = append(buf, ","...)
	buf = aterm.AppendString(buf, drv.Builder)

	buf = append(buf, ",["...)
	for i, arg := range drv.Args {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = aterm.AppendString(buf, arg)
	}

	buf = append(buf, "],["...)
	for i, k := range sortedKeys(drv.Env) {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '(')
		buf = aterm.AppendString(buf, k)
		buf = append(buf, ',')
		buf = aterm.AppendString(buf, drv.Env[k])
		buf = append(buf, ')')
	}

	buf = append(buf, "])"...)

	return buf, nil
}

func (drv *Derivation) unmarshalText(data []byte) error {
	var ok bool
	data, ok = bytes.CutPrefix(data, []byte("Derive(["))
	if !ok {
		return fmt.Errorf("parse %s derivation: file header not found", drv.Name)
	}

	clearMap(drv.Outputs)
	for {
		data, ok = bytes.CutPrefix(data, []byte("]"))
		if ok {
			break
		}
		var outName string
		var outType *DerivationOutput
		var err error
		outName, outType, data, err = parseDerivationOutput(data)
		if err != nil {
			return fmt.Errorf("parse %s derivation: %v", drv.Name, err)
		}
		if _, ok := drv.Outputs[outName]; ok {
			return fmt.Errorf("parse %s derivation: multiple outputs named %q", drv.Name, outName)
		}
		drv.Outputs[outName] = outType
	}

	// TODO(now): InputDerivations
	data, ok = bytes.CutPrefix(data, []byte(",["))
	if !ok {
		return fmt.Errorf("parse %s derivation: expected input derivations list after outputs list", drv.Name)
	}
	clearMap(drv.InputDerivations)
	for {

	}

	// TODO(now): InputSources
	// TODO(now): System
	// TODO(now): Builder
	// TODO(now): Args
	// TODO(now): Env

	if len(data) > 0 {
		return fmt.Errorf("parse %s derivation: trailing data", drv.Name)
	}
	return nil
}

type derivationOutputType int8

const (
	fixedCAOutputType derivationOutputType = 1 + iota
	floatingCAOutputType
)

// DefaultDerivationOutputName is the name of the primary output of a derivation.
// It is omitted in a number of contexts.
const DefaultDerivationOutputName = "out"

// A DerivationOutput describes the content addressing scheme of an output of a [Derivation].
type DerivationOutput struct {
	typ      derivationOutputType
	ca       nix.ContentAddress
	method   contentAddressMethod
	hashAlgo nix.HashType
}

// FixedCAOutput returns a [DerivationOutput]
// that must match the given content address assertion.
func FixedCAOutput(ca nix.ContentAddress) *DerivationOutput {
	return &DerivationOutput{
		typ: fixedCAOutputType,
		ca:  ca,
	}
}

// FlatFileFloatingCAOutput returns a [DerivationOutput]
// that must be a single file
// and will be hashed with the given algorithm.
// The hash will not be known until the derivation is realized.
func FlatFileFloatingCAOutput(hashAlgo nix.HashType) *DerivationOutput {
	return &DerivationOutput{
		typ:      floatingCAOutputType,
		method:   flatFileIngestionMethod,
		hashAlgo: hashAlgo,
	}
}

// RecursiveFileFloatingCAOutput returns a [DerivationOutput]
// that is hashed as a NAR with the given algorithm.
// The hash will not be known until the derivation is realized.
func RecursiveFileFloatingCAOutput(hashAlgo nix.HashType) *DerivationOutput {
	return &DerivationOutput{
		typ:      floatingCAOutputType,
		method:   recursiveFileIngestionMethod,
		hashAlgo: hashAlgo,
	}
}

// IsFixed reports whether the output was created by [FixedCAOutput].
func (out *DerivationOutput) IsFixed() bool {
	if out == nil {
		return false
	}
	return out.typ == fixedCAOutputType
}

// IsFloating reports whether the output's content hash cannot be known
// until the derivation is realized.
// This is true for outputs returned by
// [FlatFileFloatingCAOutput] and [RecursiveFileFloatingCAOutput].
func (out *DerivationOutput) IsFloating() bool {
	if out == nil {
		return false
	}
	return out.typ == fixedCAOutputType
}

// Path returns a fixed output's store object path
// for the given store (e.g. "/zb/store"),
// derivation name (e.g. "hello"),
// and output name (e.g. "out").
func (out *DerivationOutput) Path(store Directory, drvName, outputName string) (path Path, ok bool) {
	if out == nil {
		return "", false
	}
	switch out.typ {
	case fixedCAOutputType:
		if outputName != DefaultDerivationOutputName {
			drvName += "-" + outputName
		}
		p, err := FixedCAOutputPath(store, drvName, out.ca, References{})
		return p, err == nil
	default:
		return "", false
	}
}

func (out *DerivationOutput) marshalText(dst []byte, storeDir Directory, drvName, outName string, maskOutputs bool) ([]byte, error) {
	dst = append(dst, '(')
	dst = aterm.AppendString(dst, outName)
	if out == nil {
		dst = append(dst, `,"","","")`...)
		return dst, nil
	}
	switch out.typ {
	case fixedCAOutputType:
		if maskOutputs {
			dst = append(dst, `,""`...)
		} else {
			dst = append(dst, ',')
			p, ok := out.Path(storeDir, drvName, outName)
			if !ok {
				return dst, fmt.Errorf("marshal %s output: invalid path", outName)
			}
			dst = aterm.AppendString(dst, string(p))
		}
		dst = append(dst, ',')
		h := out.ca.Hash()
		dst = aterm.AppendString(dst, methodOfContentAddress(out.ca).prefix()+h.Type().String())
		dst = append(dst, ',')
		dst = aterm.AppendString(dst, h.RawBase16())
	case floatingCAOutputType:
		dst = append(dst, `,"",`...)
		dst = aterm.AppendString(dst, out.method.prefix()+out.hashAlgo.String())
		dst = append(dst, `,""`...)
	default:
		return dst, fmt.Errorf("marshal %s output: invalid type %v", outName, out.typ)
	}
	dst = append(dst, ')')
	return dst, nil
}

func parseDerivationOutput(data []byte) (outName string, out *DerivationOutput, tail []byte, err error) {
	var ok bool
	data, ok = bytes.CutPrefix(data, []byte("("))
	if !ok {
		return "", nil, data, fmt.Errorf("parse output: expected '('")
	}
	outName, data, err = parseATermString(data)
	if err != nil {
		return "", nil, data, fmt.Errorf("parse output: name: %v", err)
	}

	data, ok = bytes.CutPrefix(data, []byte(","))
	if !ok {
		return outName, nil, data, fmt.Errorf("parse %s output: expected ',' after name", outName)
	}
	path, data, err := parseATermString(data)
	if err != nil {
		return outName, nil, data, fmt.Errorf("parse %s output: path: %v", outName, err)
	}

	data, ok = bytes.CutPrefix(data, []byte(","))
	if !ok {
		return outName, nil, data, fmt.Errorf("parse %s output: expected ',' after path", outName)
	}
	caInfo, data, err := parseATermString(data)
	if err != nil {
		return outName, nil, data, fmt.Errorf("parse %s output: hash algorithm: %v", outName, err)
	}

	data, ok = bytes.CutPrefix(data, []byte(","))
	if !ok {
		return outName, nil, data, fmt.Errorf("parse %s output: expected ',' after hash algorithm", outName)
	}
	hashHex, data, err := parseATermString(data)
	if err != nil {
		return outName, nil, data, fmt.Errorf("parse %s output: hash: %v", outName, err)
	}

	data, ok = bytes.CutPrefix(data, []byte(")"))
	if !ok {
		return outName, nil, data, fmt.Errorf("parse %s output: expected ')' after hash", outName)
	}

	method, hashAlgo, err := parseHashAlgorithm(caInfo)
	if err != nil {
		return outName, nil, data, fmt.Errorf("parse %s output: hash algorithm: %v", outName, err)
	}
	hashBits, err := hex.DecodeString(hashHex)
	if err != nil {
		return outName, nil, data, fmt.Errorf("parse %s output: hash: %v", outName, err)
	}
	switch {
	case path == "" && hashHex == "":
		out = &DerivationOutput{
			typ:      floatingCAOutputType,
			method:   method,
			hashAlgo: hashAlgo,
		}
	case hashHex != "":
		if got, want := len(hashBits), hashAlgo.Size(); got != want {
			err = fmt.Errorf("parse %s output: hash: incorrect size (got %d bytes but %v uses %d)",
				outName, got, hashAlgo, want)
			return outName, nil, data, err
		}
		h := nix.NewHash(hashAlgo, hashBits)
		switch method {
		case flatFileIngestionMethod:
			out = FixedCAOutput(nix.FlatFileContentAddress(h))
		case recursiveFileIngestionMethod:
			out = FixedCAOutput(nix.RecursiveFileContentAddress(h))
		case textIngestionMethod:
			out = FixedCAOutput(nix.TextContentAddress(h))
		default:
			return outName, nil, data, fmt.Errorf("parse %s output: unhandled hash algorithm %q", outName, caInfo)
		}
	default:
		return outName, nil, data, fmt.Errorf("parse %s output: unknown type", outName, err)
	}
	return outName, out, data, nil
}

// makeStorePath computes a store path
// according to https://nixos.org/manual/nix/stable/protocols/store-path.
func makeStorePath(dir Directory, typ string, hash nix.Hash, name string, refs References) (Path, error) {
	h := sha256.New()
	io.WriteString(h, typ)
	for i := 0; i < refs.Others.Len(); i++ {
		io.WriteString(h, ":")
		io.WriteString(h, string(refs.Others.At(i)))
	}
	if refs.Self {
		io.WriteString(h, ":self")
	}
	io.WriteString(h, ":")
	io.WriteString(h, hash.Base16())
	io.WriteString(h, ":")
	io.WriteString(h, string(dir))
	io.WriteString(h, ":")
	io.WriteString(h, string(name))
	fingerprintHash := h.Sum(nil)
	compressed := make([]byte, 20)
	nix.CompressHash(compressed, fingerprintHash)
	digest := nixbase32.EncodeToString(compressed)
	return dir.Object(digest + "-" + name)
}

type contentAddressMethod int8

const (
	textIngestionMethod contentAddressMethod = 1 + iota
	flatFileIngestionMethod
	recursiveFileIngestionMethod
)

func methodOfContentAddress(ca nix.ContentAddress) contentAddressMethod {
	switch {
	case ca.IsText():
		return textIngestionMethod
	case ca.IsRecursiveFile():
		return recursiveFileIngestionMethod
	default:
		return flatFileIngestionMethod
	}
}

func (m contentAddressMethod) prefix() string {
	switch m {
	case textIngestionMethod:
		return "text:"
	case flatFileIngestionMethod:
		return ""
	case recursiveFileIngestionMethod:
		return "r:"
	default:
		panic("unknown content address method")
	}
}

func parseHashAlgorithm(s string) (contentAddressMethod, nix.HashType, error) {
	method := flatFileIngestionMethod
	s, ok := strings.CutPrefix(s, "r:")
	if ok {
		method = recursiveFileIngestionMethod
	} else {
		s, ok = strings.CutPrefix(s, "text:")
		if ok {
			method = textIngestionMethod
		}
	}

	typ, err := nix.ParseHashType(s)
	if err != nil {
		return method, 0, err
	}
	return method, typ, nil
}

// HashPlaceholder returns the placeholder string used in leiu of a derivation's output path.
// During a derivation's realization, the backend replaces any occurrences of the placeholder
// in the derivation's environment variables
// with the temporary output path (used until the content address stabilizes).
func HashPlaceholder(outputName string) string {
	h := nix.NewHasher(nix.SHA256)
	h.WriteString("nix-output:")
	h.WriteString(outputName)
	return "/" + h.SumHash().RawBase32()
}

// UnknownCAOutputPlaceholder returns the placeholder
// for an unknown output of a content-addressed derivation.
func UnknownCAOutputPlaceholder(drvPath Path, outputName string) string {
	drvName := strings.TrimSuffix(drvPath.Name(), DerivationExt)
	h := nix.NewHasher(nix.SHA256)
	h.WriteString("nix-upstream-output:")
	h.WriteString(drvPath.Digest())
	h.WriteString(":")
	h.WriteString(drvName)
	if outputName != DefaultDerivationOutputName {
		h.WriteString("-")
		h.WriteString(outputName)
	}
	return "/" + h.SumHash().RawBase32()
}

func sortedKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func clearMap[M ~map[K]V, K comparable, V any](m M) {
	for k := range m {
		delete(m, k)
	}
}
