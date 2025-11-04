// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"os"
	"path/filepath"
	"sync"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/tailscale/hujson"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/zbstore"
)

type globalConfig struct {
	Debug             bool                            `json:"debug"`
	Directory         zbstore.Directory               `json:"storeDirectory"`
	StoreSocket       string                          `json:"storeSocket"`
	CacheDB           string                          `json:"cacheDB"`
	AllowEnv          stringAllowList                 `json:"allowEnvironment"`
	TrustedPublicKeys []*zbstore.RealizationPublicKey `json:"trustedPublicKeys"`
}

// defaultGlobalConfig returns
func defaultGlobalConfig() *globalConfig {
	return &globalConfig{
		Directory:   zbstore.DefaultDirectory(),
		StoreSocket: filepath.Join(defaultVarDir(), "server.sock"),
	}
}

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

	if cd := cacheDir(); cd != "" {
		g.CacheDB = filepath.Join(cd, "zb", "cache.db")
	}

	return nil
}

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
		if err := jsonv2.Unmarshal(jsonData, g, jsonv2.RejectUnknownMembers(false)); err != nil {
			return fmt.Errorf("read %s: %v", path, err)
		}
	}

	return nil
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
		default:
			if reject, _ := jsonv2.GetOption(in.Options(), jsonv2.RejectUnknownMembers); reject {
				return fmt.Errorf("unmarshal config: unknown field %q", k)
			}
		}
	}
}

func (g *globalConfig) validate() error {
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

func (g *globalConfig) storeClient(opts *zbstorerpc.CodecOptions) (_ *jsonrpc.Client, wait func()) {
	var wg sync.WaitGroup
	c := jsonrpc.NewClient(func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", g.StoreSocket)
		if err != nil {
			return nil, err
		}
		return zbstorerpc.NewCodec(conn, opts), nil
	})
	return c, wg.Wait
}

// defaultVarDir returns "/opt/zb/var/zb" on Unix-like systems or `C:\zb\var\zb` on Windows systems.
func defaultVarDir() string {
	return filepath.Join(filepath.Dir(string(zbstore.DefaultDirectory())), "var", "zb")
}
