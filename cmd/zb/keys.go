// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/zbstore"
)

type privateKeyFile struct {
	Format zbstore.RealizationSignatureFormat `json:"format"`
	Key    []byte                             `json:"key,format:base64"`
}

func (f *privateKeyFile) appendToKeyring(dst *backend.Keyring) error {
	switch f.Format {
	case zbstore.Ed25519SignatureFormat:
		if got, want := len(f.Key), ed25519.SeedSize; got != want {
			return fmt.Errorf("key is wrong size (decoded is %d instead of %d bytes)", got, want)
		}
		dst.Ed25519 = append(dst.Ed25519, ed25519.NewKeyFromSeed(f.Key))
	default:
		return fmt.Errorf("unknown format %q", f.Format)
	}
	return nil
}

func readKeyringFromFiles(files []string) (*backend.Keyring, error) {
	result := new(backend.Keyring)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var parsed privateKeyFile
		if err := jsonv2.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("read %s: %v", path, err)
		}
		if err := parsed.appendToKeyring(result); err != nil {
			return nil, fmt.Errorf("read %s: %v", path, err)
		}
	}
	return result, nil
}

func newKeyCommand() *cobra.Command {
	c := &cobra.Command{
		Use:                   "key COMMAND",
		Short:                 "operate on signing key files",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.AddCommand(
		newGenerateKeyCommand(),
		newShowPublicKeyCommand(),
	)
	return c
}

func newGenerateKeyCommand() *cobra.Command {
	c := &cobra.Command{
		Use:                   "generate [-o PATH]",
		Short:                 "generate a new signing key",
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	outputPath := c.Flags().StringP("output", "o", "", "`file` to write to (default is stdout)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		outputFile := os.Stdout
		if *outputPath != "" {
			var err error
			outputFile, err = os.Create(*outputPath)
			if err != nil {
				return err
			}
		}
		err1 := runGenerateKey(cmd.Context(), outputFile)
		var err2 error
		if *outputPath != "" {
			err2 = outputFile.Close()
		}
		return cmp.Or(err1, err2)
	}
	return c
}

func runGenerateKey(ctx context.Context, dst io.Writer) error {
	_, newKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	keyFile := &privateKeyFile{
		Format: zbstore.Ed25519SignatureFormat,
		Key:    newKey.Seed(),
	}
	keyFileData, err := jsonv2.Marshal(keyFile, jsontext.Multiline(true))
	if err != nil {
		return err
	}
	keyFileData = append(keyFileData, '\n')
	_, err = dst.Write(keyFileData)
	return err
}

func newShowPublicKeyCommand() *cobra.Command {
	c := &cobra.Command{
		Use:                   "show-public [PATH [...]]",
		Short:                 "print public key of signing keys",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if len(args) == 0 {
			return runShowPublicKey(ctx, os.Stdout, os.Stdin)
		}
		for _, path := range args {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			err = runShowPublicKey(ctx, os.Stdout, f)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	return c
}

func runShowPublicKey(ctx context.Context, dst io.Writer, src io.Reader) error {
	keyFile := new(privateKeyFile)
	if err := jsonv2.UnmarshalRead(src, keyFile, jsonv2.RejectUnknownMembers(false)); err != nil {
		return err
	}
	k := new(backend.Keyring)
	if err := keyFile.appendToKeyring(k); err != nil {
		return err
	}
	var result zbstore.RealizationPublicKey
	switch {
	case len(k.Ed25519) > 0:
		result.Format = zbstore.Ed25519SignatureFormat
		result.Data = k.Ed25519[0].Public().(ed25519.PublicKey)
	default:
		return nil
	}
	data, err := jsonv2.Marshal(result, jsontext.Multiline(true))
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = dst.Write(data)
	return err
}
