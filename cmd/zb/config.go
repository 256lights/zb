// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/tailscale/hujson"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/netrc"
	"zb.256lights.llc/pkg/internal/remotestore"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/zbstore"
)

// globalConfig is the set of configuration settings and persistent command-line flags.
// More details at https://main--zb-docs.netlify.app/configuration
type globalConfig struct {
	Debug             bool                            `json:"debug" kong:"help=Show debugging output."`
	Directory         zbstore.Directory               `json:"storeDirectory" kong:"name=store,default=${default_store_dir},help=Store directory"`
	StoreSocket       string                          `json:"storeSocket" kong:"default=${default_store_socket},help=Server socket"`
	NetrcPath         string                          `json:"netrcFile,omitempty" kong:"name=netrc-file,default=${netrc},help=Use HTTP credentials from the given path."`
	CacheDB           string                          `json:"cacheDB" kong:"name=cache,default=${cache_db},help=Cache database"`
	AllowEnv          stringAllowList                 `json:"allowEnvironment" kong:"-"`
	TrustedPublicKeys []*zbstore.RealizationPublicKey `json:"trustedPublicKeys" kong:"-"`
	Server            serverConfig                    `json:"server,omitzero" kong:"-"`
}

// defaultGlobalConfig returns a [globalConfig] populated with values
// based on OS and generic environment variables (e.g. $HOME, $XDG_CACHE_HOME, etc.).
func defaultGlobalConfig() *globalConfig {
	g := &globalConfig{
		Directory:   zbstore.DefaultDirectory(),
		StoreSocket: filepath.Join(defaultVarDir(), "server.sock"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		g.NetrcPath = filepath.Join(home, ".netrc")
	}
	if cd := cacheDir(); cd != "" {
		g.CacheDB = filepath.Join(cd, "zb", "cache.db")
	}
	return g
}

// mergeEnvironment copies environment variable values to [globalConfig] fields.
func (g *globalConfig) mergeEnvironment() error {
	if dir := os.Getenv("ZB_STORE_DIR"); dir != "" {
		zbDir, err := zbstore.CleanDirectory(dir)
		if err != nil {
			return err
		}
		g.Directory = zbDir
	}

	if path := os.Getenv("ZB_STORE_SOCKET"); path != "" {
		g.StoreSocket = path
	}

	if path := os.Getenv("NETRC"); path != "" {
		g.NetrcPath = path
	}

	return nil
}

// mergeFiles parses each path as JSON With Commas and Comments
// and merges each into g.
// Thus, later files in the paths sequence take precedence over earlier files.
func (g *globalConfig) mergeFiles(paths iter.Seq[string]) error {
	for path := range paths {
		huJSONData, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		jsonData, err := hujson.Standardize(huJSONData)
		if err != nil {
			return fmt.Errorf("read %s: %v", path, err)
		}
		prev := new(*g)
		if err := jsonv2.Unmarshal(jsonData, g, jsonv2.RejectUnknownMembers(false)); err != nil {
			return fmt.Errorf("read %s: %v", path, err)
		}
		g.resolveRelativePaths(filepath.Dir(path), prev)
	}

	return nil
}

func (g *globalConfig) resolveRelativePaths(dir string, prev *globalConfig) {
	resolve := func(path string) string {
		if !filepath.IsAbs(path) {
			return filepath.Join(dir, path)
		}
		return path
	}
	if prev == nil || g.StoreSocket != prev.StoreSocket {
		g.StoreSocket = resolve(g.StoreSocket)
	}
	if prev == nil || g.CacheDB != prev.CacheDB {
		g.CacheDB = resolve(g.CacheDB)
	}
	if prev == nil || g.NetrcPath != prev.NetrcPath {
		g.NetrcPath = resolve(g.NetrcPath)
	}
}

// UnmarshalJSONFrom unmarshals the configuration object from the JSON decoder,
// merging any fields in the JSON object with existing values.
func (g *globalConfig) UnmarshalJSONFrom(in *jsontext.Decoder) error {
	tok, err := in.ReadToken()
	if err != nil {
		return err
	}
	if got := tok.Kind(); got != '{' {
		return fmt.Errorf("config must be an object not a %v", got)
	}

	for {
		keyToken, err := in.ReadToken()
		if err != nil {
			return err
		}
		switch kind := keyToken.Kind(); kind {
		case '}':
			return nil
		case '"':
			// Keep going.
		default:
			return fmt.Errorf("unexpected non-string key (%v) in object", kind)
		}

		switch k := keyToken.String(); k {
		case "debug":
			if err := jsonv2.UnmarshalDecode(in, &g.Debug); err != nil {
				return fmt.Errorf("unmarshal config.debug: %w", err)
			}
		case "storeDirectory":
			if err := jsonv2.UnmarshalDecode(in, &g.Directory); err != nil {
				return fmt.Errorf("unmarshal config.storeDirectory: %w", err)
			}
		case "storeSocket":
			if err := jsonv2.UnmarshalDecode(in, &g.StoreSocket); err != nil {
				return fmt.Errorf("unmarshal config.storeSocket: %w", err)
			}
		case "cacheDB":
			if err := jsonv2.UnmarshalDecode(in, &g.CacheDB); err != nil {
				return fmt.Errorf("unmarshal config.cacheDB: %w", err)
			}
		case "allowEnvironment":
			if err := jsonv2.UnmarshalDecode(in, &g.AllowEnv); err != nil {
				return fmt.Errorf("unmarshal config.allowEnvironment: %w", err)
			}
		case "trustedPublicKeys":
			// Use any unused capacity at end of the slice.
			newKeys := g.TrustedPublicKeys[len(g.TrustedPublicKeys):]

			if err := jsonv2.UnmarshalDecode(in, &newKeys); err != nil {
				return fmt.Errorf("unmarshal config.trustedPublicKeys: %w", err)
			}
			g.TrustedPublicKeys = append(g.TrustedPublicKeys, newKeys...)
		case "netrcFile":
			if err := jsonv2.UnmarshalDecode(in, &g.NetrcPath); err != nil {
				return fmt.Errorf("unmarshal config.netrcFile: %w", err)
			}
		case "server":
			if err := jsonv2.UnmarshalDecode(in, &g.Server); err != nil {
				return fmt.Errorf("unmarshal config.server: %w", err)
			}
		default:
			if reject, _ := jsonv2.GetOption(in.Options(), jsonv2.RejectUnknownMembers); reject {
				return fmt.Errorf("unmarshal config: unknown field %q", k)
			}
		}
	}
}

// Validate checks the configuration for any missing or semantically incorrect settings.
// Validate should be called after the configuration is complete,
// because partial configurations may not pass validation.
func (g *globalConfig) Validate() error {
	if !filepath.IsAbs(string(g.Directory)) {
		// The directory must be in the format of the local OS.
		return fmt.Errorf("store directory %q is not absolute", g.Directory)
	}
	if g.StoreSocket == "" {
		return fmt.Errorf("ZB_STORE_SOCKET not set")
	}
	if g.CacheDB == "" {
		return fmt.Errorf("cache directory not set")
	}

	return nil
}

func (g *globalConfig) reusePolicy() *zbstorerpc.ReusePolicy {
	if len(g.TrustedPublicKeys) == 0 {
		return &zbstorerpc.ReusePolicy{All: true}
	}
	return &zbstorerpc.ReusePolicy{PublicKeys: g.TrustedPublicKeys}
}

func (g *globalConfig) newHTTPClient() (*http.Client, error) {
	if g.NetrcPath == "" {
		return http.DefaultClient, nil
	}
	netrcData, err := os.ReadFile(g.NetrcPath)
	if errors.Is(err, os.ErrNotExist) {
		return http.DefaultClient, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open netrc file: %v", err)
	}
	return &http.Client{
		Transport: &netrc.Transport{
			Netrc:        netrcData,
			RoundTripper: http.DefaultTransport,
		},
	}, nil
}

func (g *globalConfig) storeClient(opts *zbstorerpc.CodecOptions) *jsonrpc.Client {
	return jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", g.StoreSocket)
		if err != nil {
			return nil, err
		}
		return zbstorerpc.NewCodec(conn, opts), nil
	})
}

func (g *globalConfig) storeDeps() (_ *storeDeps, cleanup func()) {
	var state struct {
		client *http.Client
	}

	deps := &storeDeps{
		httpClientProvider: func() (*http.Client, error) {
			if state.client == nil {
				var err error
				state.client, err = g.newHTTPClient()
				if err != nil {
					return state.client, err
				}
			}
			return state.client, nil
		},
	}
	cleanup = func() {
		if state.client != nil {
			state.client.CloseIdleConnections()
		}
	}
	return deps, cleanup
}

type storeDeps struct {
	httpClientProvider func() (*http.Client, error)
}

type storeConfig struct {
	Type       string         `json:"type"`
	Properties jsontext.Value `json:",inline"`
}

func (sc *storeConfig) toStore(deps *storeDeps) (backend.Store, error) {
	if sc == nil {
		return zbstore.Null{}, nil
	}
	switch sc.Type {
	case "null":
		return zbstore.Null{}, nil
	case "http":
		var props struct {
			URL string `json:"url"`
		}
		if err := jsonv2.Unmarshal(sc.Properties, &props); err != nil {
			return nil, fmt.Errorf("unmarshal http store configuration: %v", err)
		}
		client, err := deps.httpClientProvider()
		if err != nil {
			return nil, err
		}
		store := &remotestore.HTTPStore{
			HTTPClient: client,
		}
		store.URL, err = url.Parse(props.URL)
		if err != nil {
			return nil, fmt.Errorf("unmarshal http store configuration: url: %v", err)
		}
		return store, nil
	default:
		return nil, fmt.Errorf("unmarshal store configuration: unknown type %q", sc.Type)
	}
}

// defaultVarDir returns "/opt/zb/var/zb" on Unix-like systems or `C:\zb\var\zb` on Windows systems.
func defaultVarDir() string {
	return filepath.Join(filepath.Dir(string(zbstore.DefaultDirectory())), "var", "zb")
}
