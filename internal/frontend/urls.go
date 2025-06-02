// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	slashpath "path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"zb.256lights.llc/pkg/internal/lua"
	"zb.256lights.llc/pkg/internal/lualex"
	"zb.256lights.llc/pkg/internal/system"
	"zb.256lights.llc/pkg/internal/useragent"
	"zb.256lights.llc/pkg/internal/xio"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

// URLs imports the Lua file for each URL,
// and uses the fragment from each URL (see [parseFragment])
// to determine the Lua value to return.
func (eval *Eval) URLs(ctx context.Context, urls []string) ([]any, error) {
	if len(urls) == 0 {
		return nil, nil
	}

	// Parse URLs first before doing any expensive operations.
	parsedURLs := make([]*url.URL, len(urls))
	for i, s := range urls {
		u, err := ParseURL(s)
		if err != nil {
			return nil, err
		}
		archiveEntry, _, err := parseFragment(u.Fragment)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", s, err)
		}
		if u.Scheme == "" || u.Scheme == "file" {
			if _, err := URLToPath(u); err != nil {
				return nil, err
			}
			if archiveEntry != "" {
				return nil, fmt.Errorf("%s: archive path not valid for local file", s)
			}
		}
		parsedURLs[i] = u
	}

	// Download and import any URLs.
	grp, grpCtx := errgroup.WithContext(ctx)
	grp.SetLimit(2)
	var mu sync.Mutex
	importedStorePaths := make(map[string]zbstore.Path, len(parsedURLs))
	for _, u := range parsedURLs {
		if u.Scheme == "" || u.Scheme == "file" {
			continue
		}
		key := stripFragment(u).String()
		mu.Lock()
		if _, ok := importedStorePaths[key]; ok {
			mu.Unlock()
			continue
		}
		importedStorePaths[key] = ""
		mu.Unlock()

		grp.Go(func() error {
			path, err := eval.importURL(grpCtx, u)
			if err != nil {
				return err
			}

			mu.Lock()
			importedStorePaths[key] = path
			mu.Unlock()
			return nil
		})
	}
	if err := grp.Wait(); err != nil {
		return nil, err
	}

	// Start imports. These will run concurrently.
	l, err := eval.newState()
	if err != nil {
		return nil, err
	}
	defer l.Close()
	l.CreateTable(len(parsedURLs), 0)
	tableStackIndex := l.Top()
	if _, err := l.Global(ctx, "import"); err != nil {
		return nil, fmt.Errorf("internal error: _G.import: %v", err)
	}
	importStackIndex := l.Top()
	if _, err := l.Global(ctx, "extract"); err != nil {
		return nil, fmt.Errorf("internal error: _G.extract: %v", err)
	}
	extractStackIndex := l.Top()
	for i, u := range parsedURLs {
		l.PushValue(importStackIndex)
		if u.Scheme == "" || u.Scheme == "file" {
			path, err := URLToPath(u)
			if err != nil {
				// Should have already been verified above.
				return nil, fmt.Errorf("internal error: %v", err)
			}
			l.PushString(path)
		} else {
			storePath := importedStorePaths[stripFragment(u).String()]
			l.PushStringContext(string(storePath), sets.New(contextValue{path: storePath}.String()))
			if archiveFile, _, _ := parseFragment(u.Fragment); archiveFile != "" {
				// Call extract{src=storePath}.
				l.CreateTable(0, 1)
				l.Insert(-2)
				if err := l.RawSetField(-2, "src"); err != nil {
					return nil, fmt.Errorf("internal error: {src=%s}: %v", lualex.Quote(string(storePath)), err)
				}
				l.PushValue(extractStackIndex)
				l.Insert(-2)
				if err := l.PCall(ctx, 1, 1, 0); err != nil {
					return nil, fmt.Errorf("extract{src=%s}: %v", lualex.Quote(string(storePath)), err)
				}
				l.PushString("/" + archiveFile)
				if err := l.Concat(ctx, 2); err != nil {
					return nil, fmt.Errorf("internal error: concat extract{...}..%s: %v",
						lualex.Quote(archiveFile), err)
				}
			}
		}
		if err := l.PCall(ctx, 1, 1, 0); err != nil {
			return nil, err
		}
		l.RawSetIndex(tableStackIndex, int64(i+1))
	}

	// Perform lookups on each import.
	result := make([]any, len(urls))
	sys := system.Current()
	sysTriple := sys.Arch.String() + "-" + sys.Vendor.String() + "-" + sys.OS.String()
	if !sys.Env.IsUnknown() && !(sys.Env == "msvc" && sys.OS.IsWindows()) {
		sysTriple += "-" + sys.Env.String()
	}
	l.PushClosure(0, messageHandler)
	for i, u := range parsedURLs {
		l.RawIndex(tableStackIndex, int64(i+1))
		_, fieldPath, _ := parseFragment(u.Fragment)
		if fieldPath == "" {
			l.PushValue(-1)
		} else {
			if err := searchKeyPaths(ctx, l, fieldPath, []string{sysTriple}, -2); err != nil {
				return nil, fmt.Errorf("%s: %v", urls[i], err)
			}
		}
		val, err := luaToGo(ctx, l)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", urls[i], err)
		}
		result[i] = val
		l.Pop(2)
	}
	return result, nil
}

func (eval *Eval) importURL(ctx context.Context, u *url.URL) (zbstore.Path, error) {
	u = stripFragment(u)
	req := &http.Request{
		Method: http.MethodGet,
		URL:    u,
		Header: http.Header{
			"Accept":     {"text/plain, application/gzip, application/x-bzip2, application/zip, application/x-tar;q=0.9"},
			"User-Agent": {useragent.String},
		},
	}
	resp, err := eval.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("download %v: %v", u, err)
	}
	respCloser := xio.CloseOnce(resp.Body)
	defer respCloser.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %v: http %s", u, resp.Status)
	}

	// If the server provides a Content-Length header,
	// we can stream the download directly to the store.
	name := inferDownloadName(slashpath.Base(u.Path))
	if resp.ContentLength >= 0 {
		path, err := eval.importFlatFile(ctx, name, resp.ContentLength, resp.Body)
		if err != nil {
			return "", fmt.Errorf("download %v: %v", u, err)
		}
		return path, nil
	}

	// Otherwise, download to a temporary file and then ingest.
	f, err := eval.downloadTemp.CreateBuffer(-1)
	if err != nil {
		return "", fmt.Errorf("download %v: %v", u, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Debugf(ctx, "Closing temp file: %v", err)
		}
	}()
	size, err := io.Copy(f, resp.Body)
	respCloser.Close()
	if err != nil {
		return "", fmt.Errorf("download %v: %v", u, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("download %v: %v", u, err)
	}
	path, err := eval.importFlatFile(ctx, name, size, f)
	if err != nil {
		return "", fmt.Errorf("download %v: %v", u, err)
	}
	return path, nil
}

func (eval *Eval) importFlatFile(ctx context.Context, name string, size int64, f io.Reader) (zbstore.Path, error) {
	exporter, closeExport, err := startExport(ctx, eval.store)
	if err != nil {
		return "", fmt.Errorf("import %s: %v", name, err)
	}
	defer closeExport(false)
	nw := nar.NewWriter(exporter)
	if err := nw.WriteHeader(&nar.Header{Size: size}); err != nil {
		return "", fmt.Errorf("import %s: %v", name, err)
	}
	h := nix.NewHasher(nix.SHA256)
	if _, err := io.CopyN(io.MultiWriter(h, nw), f, size); err != nil {
		return "", fmt.Errorf("import %s: %v", name, err)
	}
	if err := nw.Close(); err != nil {
		return "", fmt.Errorf("import %s: %v", name, err)
	}
	ca := nix.FlatFileContentAddress(h.SumHash())
	path, err := zbstore.FixedCAOutputPath(eval.storeDir, name, ca, zbstore.References{})
	if err != nil {
		return "", fmt.Errorf("import %s: %v", name, err)
	}
	err = exporter.Trailer(&zbstore.ExportTrailer{
		StorePath:      path,
		ContentAddress: ca,
	})
	if err != nil {
		return "", fmt.Errorf("import %s: %v", name, err)
	}
	if err := closeExport(true); err != nil {
		return "", fmt.Errorf("import %s: %v", name, err)
	}
	return path, nil
}

// searchKeyPaths pushes the value at the slash-separated field path onto the stack.
// If fieldPath as written results in nil or triggers an error,
// then the field paths in prefixes are searched in-order.
// If no non-nil value could be found, searchKeyPaths returns the first error encountered.
//
// If msgHandler is non-zero, it is used to format the error
// but nothing will be pushed onto the stack.
func searchKeyPaths(ctx context.Context, l *lua.State, fieldPath string, prefixes []string, msgHandler int) error {
	if !l.CheckStack(4) {
		return errors.New("internal error: lua stack overflow")
	}
	if msgHandler != 0 {
		msgHandler = l.AbsIndex(msgHandler)
	}
	tableIndex := l.Top()

	// Push the function onto the stack once, then clone as needed.
	l.PushClosure(0, followKeyPath)
	followFieldPathIndex := tableIndex + 1
	// Remove followFieldPath function before returning to caller.
	defer l.Remove(followFieldPathIndex)

	// Try the fieldPath as written first.
	l.PushValue(followFieldPathIndex)
	l.PushValue(tableIndex)
	l.PushString(fieldPath)
	firstError := l.PCall(ctx, 2, 1, msgHandler)
	if firstError == nil && l.Type(-1) != lua.TypeNil {
		// Found!
		log.Debugf(ctx, "Found non-nil at %s", fieldPath)
		return nil
	}
	log.Debugf(ctx, "%s not found", fieldPath)
	l.SetTop(followFieldPathIndex)

	// Try alternative prefixes.
	for _, prefix := range prefixes {
		l.PushValue(followFieldPathIndex)
		l.PushValue(tableIndex)
		l.PushString(prefix + "/" + fieldPath)

		if err := l.PCall(ctx, 2, 1, msgHandler); err != nil {
			if msgHandler != 0 {
				l.Pop(1)
			}
			if firstError == nil {
				firstError = fmt.Errorf("in %s/%s: %w", prefix, fieldPath, err)
			} else {
				log.Debugf(ctx, "Error when trying %s/%s: %v", prefix, fieldPath, err)
			}
		} else if l.Type(-1) == lua.TypeNil {
			log.Debugf(ctx, "%s/%s not found", prefix, fieldPath)
			l.Pop(1)
		} else {
			// Found!
			log.Debugf(ctx, "Found non-nil at %s/%s", prefix, fieldPath)
			return nil
		}
	}
	l.PushNil()
	return firstError
}

// followKeyPath is a [lua.Function] that accesses a slash-separated field path
// and returns the value.
// If a nil is encountered along the way, nil is returned.
// The first argument to followKeyPath is the root object
// and the second argument is the string containing the slash-separated field path.
func followKeyPath(ctx context.Context, l *lua.State) (int, error) {
	fieldPath, err := lua.CheckString(l, 2)
	if err != nil {
		return 0, err
	}
	l.SetTop(1)

	lastType := l.Type(-1)
	for k := range splitKeyPath(fieldPath) {
		if lastType == lua.TypeNil {
			return 1, nil
		}
		var err error
		lastType, err = l.Field(ctx, -1, k)
		if err != nil {
			return 0, err
		}
		l.Remove(-2)
	}
	return 1, nil
}

// ParseURL parses a URL, but permits some amount of sloppiness for Windows paths.
func ParseURL(s string) (*url.URL, error) {
	if filepath.VolumeName(s) != "" {
		i := strings.IndexByte(s, '#')
		if i < 0 {
			i = len(s)
		}
		path, err := url.PathUnescape(s[:i])
		if err != nil {
			return nil, err
		}
		if i >= len(s)-1 {
			return &url.URL{Path: path, RawPath: s[:i]}, nil
		}

		u, err := url.Parse(s[i:])
		if err != nil {
			return nil, err
		}
		u.Path, err = url.PathUnescape(s[:i])
		if err != nil {
			return nil, err
		}
		u.RawPath = s[:i]
		return u, nil
	}

	return url.Parse(s)
}

// URLToPath returns the filesystem path represented by u.
// URLToPath returns an error if u is not a "file:" URL or a path.
func URLToPath(u *url.URL) (string, error) {
	if u.Scheme == "" {
		return strings.ReplaceAll(u.Path, "/", string(filepath.Separator)), nil
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("%v is not a file:// URL", u)
	}
	if runtime.GOOS == "windows" {
		path := `\\`
		if u.Host != "" {
			path += u.Host
		} else {
			path += "localhost"
		}
		path += strings.ReplaceAll(u.Path, "/", `\`)
		return path, nil
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("cannot use %s in file:// URL", u.Host)
	}
	return strings.ReplaceAll(u.Path, "/", string(filepath.Separator)), nil
}

// parseFragment parses an unescaped fragment string (excluding the "#")
// and splits it at the last colon (":") into an archive member path
// and a slash-separated key path.
// parseFragment returns an error if the keyPath has leading, trailing, or repeated dots.
func parseFragment(s string) (archivePath, keyPath string, err error) {
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		archivePath = s[:i]
		keyPath = s[i+1:]
	} else {
		keyPath = s
	}

	if keyPath == "" {
		return archivePath, keyPath, nil
	}
	for k := range splitKeyPath(keyPath) {
		if k == "" {
			return archivePath, keyPath, fmt.Errorf("invalid fragment %v: empty key in %s", &url.URL{Fragment: s}, keyPath)
		}
	}
	return archivePath, keyPath, nil
}

// splitKeyPath splits a slash-separated path into its components.
// A run of slashes is treated the same as a single slash.
// An empty string yields no elements.
func splitKeyPath(s string) iter.Seq[string] {
	if s == "" {
		return func(yield func(string) bool) {}
	}
	return func(yield func(string) bool) {
		for i := 0; ; {
			n := strings.IndexByte(s[i:], '/')
			if n < 0 {
				yield(s[i:])
				return
			}

			if !yield(s[i : i+n]) {
				return
			}
			i = i + n
			for i < len(s) && s[i] == '/' {
				i++
			}
		}
	}
}

// inferDownloadName converts baseName into a store-object-appropriate name.
func inferDownloadName(baseName string) string {
	hasNameChars := false
	hasSpecialChars := false
	for _, c := range baseName {
		if c <= 0x7f && isNameChar(byte(c)) {
			hasNameChars = true
		} else {
			hasSpecialChars = true
		}
	}
	if !hasNameChars {
		return "x"
	}
	if !hasSpecialChars {
		return baseName
	}

	sb := new(strings.Builder)
	sb.Grow(len(baseName))
	for _, c := range baseName {
		if c <= 0x7f && isNameChar(byte(c)) {
			hasNameChars = true
			sb.WriteByte(byte(c))
		} else {
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

func isNameChar(c byte) bool {
	return 'a' <= c && c <= 'z' ||
		'A' <= c && c <= 'Z' ||
		'0' <= c && c <= '9' ||
		c == '+' || c == '-' || c == '.' || c == '_' || c == '='
}
