package zb

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"slices"
	"strings"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/nix/nixbase32"
)

// A Derivation represents a store derivation:
// a single, specific, constant build action.
type Derivation struct {
	// Dir is the store directory this derivation is a part of.
	Dir nix.StoreDirectory

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
	InputSources []nix.StorePath
	// InputDerivations is the set of derivations that this derivation depends on.
	// The mapped values are the set of output names that are used.
	InputDerivations map[nix.StorePath]map[string]struct{}
	// Outputs is the set of outputs that the derivation produces.
	Outputs map[string]*DerivationOutput
}

func (drv *Derivation) StorePath() (nix.StorePath, error) {
	if drv.Name == "" {
		return "", fmt.Errorf("compute derivation path: missing name")
	}
	p, _, err := drv.export()
	if err != nil {
		return "", fmt.Errorf("compute %s derivation path: %v", drv.Name, err)
	}
	return p, nil
}

func (drv *Derivation) export() (nix.StorePath, []byte, error) {
	if drv.Name == "" {
		return "", nil, fmt.Errorf("missing name")
	}
	if drv.Dir == "" {
		return "", nil, fmt.Errorf("missing store directory")
	}

	data, err := drv.marshalText(false)
	if err != nil {
		return "", nil, err
	}
	h := nix.NewHasher(nix.SHA256)
	h.Write(data)

	p, err := fixedCAOutputPath(
		drv.Dir,
		drv.Name+".drv",
		nix.TextContentAddress(h.SumHash()),
		drv.references(),
	)
	if err != nil {
		return "", data, err
	}
	return p, data, nil
}

func (drv *Derivation) references() storeReferences {
	refs := storeReferences{
		others: make(map[nix.StorePath]struct{}, len(drv.InputSources)+len(drv.InputDerivations)),
	}
	for _, src := range drv.InputSources {
		refs.others[src] = struct{}{}
	}
	for _, input := range sortedKeys(drv.InputDerivations) {
		refs.others[input] = struct{}{}
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
		buf = appendATermString(buf, string(drvPath))
		buf = append(buf, ",["...)
		// TODO(someday): This can be some kind of tree? See DerivedPathMap.
		for j, outName := range sortedKeys(drv.InputDerivations[drvPath]) {
			if j > 0 {
				buf = append(buf, ',')
			}
			buf = appendATermString(buf, outName)
		}
		buf = append(buf, "])"...)
	}

	buf = append(buf, "],["...)
	srcs := drv.InputSources
	if !slices.IsSorted(srcs) {
		srcs = slices.Clone(srcs)
		slices.Sort(srcs)
	}
	for i, src := range srcs {
		if i > 0 {
			buf = append(buf, ',')
		}
		if got := src.Dir(); got != drv.Dir {
			return nil, fmt.Errorf("marshal %s derivation: inputs: unexpected store directory %s (using %s)",
				drv.Name, got, drv.Dir)
		}
		buf = appendATermString(buf, string(src))
	}

	buf = append(buf, "],"...)
	buf = appendATermString(buf, drv.System)
	buf = append(buf, ","...)
	buf = appendATermString(buf, drv.Builder)

	buf = append(buf, ",["...)
	for i, arg := range drv.Args {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendATermString(buf, arg)
	}

	buf = append(buf, "],["...)
	for i, k := range sortedKeys(drv.Env) {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '(')
		buf = appendATermString(buf, k)
		buf = append(buf, ',')
		buf = appendATermString(buf, drv.Env[k])
		buf = append(buf, ')')
	}

	buf = append(buf, "])"...)

	return buf, nil
}

func writeDerivation(ctx context.Context, drv *Derivation) (nix.StorePath, error) {
	p, data, err := drv.export()
	if err != nil {
		if drv.Name == "" {
			return "", fmt.Errorf("write derivation: %v", err)
		}
		return "", fmt.Errorf("write %s derivation: %v", drv.Name, err)
	}

	imp, err := startImport(ctx)
	if err != nil {
		return "", fmt.Errorf("write %s derivation: %v", drv.Name, err)
	}
	defer imp.Close()
	w := nar.NewWriter(imp)
	err = w.WriteHeader(&nar.Header{
		Size: int64(len(data)),
	})
	if err != nil {
		return "", fmt.Errorf("write %s derivation: %v", drv.Name, err)
	}
	if _, err := w.Write(data); err != nil {
		return "", fmt.Errorf("write %s derivation: %v", drv.Name, err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("write %s derivation: %v", drv.Name, err)
	}
	err = imp.Trailer(&nixExportTrailer{
		storePath:  p,
		references: sortedKeys(drv.references().others),
	})
	if err != nil {
		return "", fmt.Errorf("write %s derivation: %v", drv.Name, err)
	}

	if err := imp.Close(); err != nil {
		return "", fmt.Errorf("write %s derivation: %v", drv.Name, err)
	}
	return p, nil
}

type derivationOutputType int8

const (
	deferredOutputType derivationOutputType = 1 + iota
	inputAddressedOutputType
	fixedCAOutputType
	floatingCAOutputType
)

const defaultDerivationOutputName = "out"

// A DerivationOutput is an output of a [Derivation].
// A nil DerivationOutput represents a deferred output.
type DerivationOutput struct {
	typ      derivationOutputType
	path     nix.StorePath
	ca       nix.ContentAddress
	method   contentAddressMethod
	hashAlgo nix.HashType
}

func InputAddressed(path nix.StorePath) *DerivationOutput {
	return &DerivationOutput{
		typ:  inputAddressedOutputType,
		path: path,
	}
}

func FixedCAOutput(ca nix.ContentAddress) *DerivationOutput {
	return &DerivationOutput{
		typ: fixedCAOutputType,
		ca:  ca,
	}
}

func TextFloatingCAOutput(hashAlgo nix.HashType) *DerivationOutput {
	return &DerivationOutput{
		typ:      floatingCAOutputType,
		method:   textIngestionMethod,
		hashAlgo: hashAlgo,
	}
}

func FlatFileFloatingCAOutput(hashAlgo nix.HashType) *DerivationOutput {
	return &DerivationOutput{
		typ:      floatingCAOutputType,
		method:   flatFileIngestionMethod,
		hashAlgo: hashAlgo,
	}
}

func RecursiveFileFloatingCAOutput(hashAlgo nix.HashType) *DerivationOutput {
	return &DerivationOutput{
		typ:      floatingCAOutputType,
		method:   recursiveFileIngestionMethod,
		hashAlgo: hashAlgo,
	}
}

func (out *DerivationOutput) Path(store nix.StoreDirectory, drvName, outputName string) (path nix.StorePath, ok bool) {
	if out == nil {
		return "", false
	}
	switch out.typ {
	case inputAddressedOutputType:
		return out.path, true
	case fixedCAOutputType:
		if outputName != defaultDerivationOutputName {
			drvName += "-" + outputName
		}
		p, err := fixedCAOutputPath(store, drvName, out.ca, storeReferences{})
		return p, err == nil
	default:
		return "", false
	}
}

func (out *DerivationOutput) marshalText(dst []byte, storeDir nix.StoreDirectory, drvName, outName string, maskOutputs bool) ([]byte, error) {
	dst = append(dst, '(')
	dst = appendATermString(dst, outName)
	if out == nil {
		dst = append(dst, `,"","","")`...)
		return dst, nil
	}
	switch out.typ {
	case inputAddressedOutputType:
		if maskOutputs {
			dst = append(dst, `,""`...)
		} else {
			if got := out.path.Dir(); got != storeDir {
				return dst, fmt.Errorf("marshal %s output: unexpected store directory %s (using %s)",
					outName, got, storeDir)
			}
			dst = append(dst, ',')
			dst = appendATermString(dst, string(out.path))
		}
		dst = append(dst, `,"",""`...)
	case fixedCAOutputType:
		if maskOutputs {
			dst = append(dst, `,""`...)
		} else {
			dst = append(dst, ',')
			p, ok := out.Path(storeDir, drvName, outName)
			if !ok {
				return dst, fmt.Errorf("marshal %s output: invalid path", outName)
			}
			dst = appendATermString(dst, string(p))
		}
		dst = append(dst, ',')
		h := out.ca.Hash()
		dst = appendATermString(dst, methodOfContentAddress(out.ca).prefix()+h.Type().String())
		dst = append(dst, ',')
		dst = appendATermString(dst, h.RawBase16())
	case floatingCAOutputType:
		dst = append(dst, `,"",`...)
		dst = appendATermString(dst, out.method.prefix()+out.hashAlgo.String())
		dst = append(dst, `,""`...)
	default:
		return dst, fmt.Errorf("marshal %s output: invalid type %v", outName, out.typ)
	}
	dst = append(dst, ')')
	return dst, nil
}

// makeStorePath computes a store path
// according to https://nixos.org/manual/nix/stable/protocols/store-path.
func makeStorePath(dir nix.StoreDirectory, typ string, hash nix.Hash, name string, refs storeReferences) (nix.StorePath, error) {
	h := sha256.New()
	io.WriteString(h, typ)
	for _, ref := range sortedKeys(refs.others) {
		io.WriteString(h, ":")
		io.WriteString(h, string(ref))
	}
	if refs.self {
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

func fixedCAOutputPath(dir nix.StoreDirectory, name string, ca nix.ContentAddress, refs storeReferences) (nix.StorePath, error) {
	h := ca.Hash()
	htype := h.Type()
	switch {
	case ca.IsText():
		if want := nix.SHA256; htype != want {
			return "", fmt.Errorf("compute fixed output path for %s: text must be content-addressed by %v (got %v)",
				name, want, htype)
		}
		return makeStorePath(dir, "text", h, name, refs)
	case htype == nix.SHA256 && ca.IsRecursiveFile():
		return makeStorePath(dir, "source", h, name, refs)
	default:
		if !refs.isEmpty() {
			return "", fmt.Errorf("compute fixed output path for %s: references not allowed", name)
		}
		h2 := nix.NewHasher(nix.SHA256)
		h2.WriteString("fixed:out:")
		h2.WriteString(methodOfContentAddress(ca).prefix())
		h2.WriteString(h.Base16())
		h2.WriteString(":")
		return makeStorePath(dir, "output:out", h2.SumHash(), name, storeReferences{})
	}
}

type storeReferences struct {
	self   bool
	others map[nix.StorePath]struct{}
}

func (refs storeReferences) isEmpty() bool {
	return !refs.self && len(refs.others) == 0
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

func hashPlaceholder(outputName string) string {
	h := nix.NewHasher(nix.SHA256)
	h.WriteString("nix-output:")
	h.WriteString(outputName)
	return "/" + h.SumHash().RawBase32()
}

// unknownCAOutputPlaceholder returns the placeholder
// for an unknown output of a content-addressed derivation.
func unknownCAOutputPlaceholder(drvPath nix.StorePath, outputName string) string {
	drvName := strings.TrimSuffix(drvPath.Name(), ".drv")
	h := nix.NewHasher(nix.SHA256)
	h.WriteString("nix-upstream-output:")
	h.WriteString(drvPath.Digest())
	h.WriteString(":")
	h.WriteString(drvName)
	if outputName != defaultDerivationOutputName {
		h.WriteString("-")
		h.WriteString(outputName)
	}
	return "/" + h.SumHash().RawBase32()
}

func appendATermString(dst []byte, s string) []byte {
	size := len(s) + len(`""`)
	for _, c := range []byte(s) {
		if c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' {
			size++
		}
	}

	dst = slices.Grow(dst, size)
	dst = append(dst, '"')
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\n':
			dst = append(dst, `\n`...)
		case '\r':
			dst = append(dst, `\r`...)
		case '\t':
			dst = append(dst, `\t`...)
		default:
			dst = append(dst, c)
		}
	}
	dst = append(dst, '"')
	return dst
}

func sortedKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
