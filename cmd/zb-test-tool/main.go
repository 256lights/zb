// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"sync"

	"github.com/alecthomas/kong"
	kongcompletion "github.com/jotaen/kong-completion"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix/nixbase32"
)

type command struct {
	Debug          bool                   `kong:"help=Show debugging output."`
	GenerateDigest *generateDigestCommand `kong:"cmd"`
	Derivation     *derivationCommand     `kong:"cmd"`
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
	return `kong:"help=Generate a random object digest"`
}

func (c *generateDigestCommand) Run(kc *kong.Context) error {
	const digestSize = 32
	entropy := make([]byte, nixbase32.DecodedLen(digestSize))
	if _, err := rand.Read(entropy); err != nil {
		return err
	}
	buf := make([]byte, digestSize+len("\n"))
	nixbase32.Encode(buf, entropy)
	buf[len(buf)-1] = '\n'
	_, err := kc.Stdout.Write(buf)
	return err
}

type derivationCommand struct {
	InputPlaceholder  *derivationInputPlaceholderCommand  `kong:"cmd"`
	OutputPlaceholder *derivationOutputPlaceholderCommand `kong:"cmd"`
}

type derivationInputPlaceholderCommand struct {
	OutputReference zbstore.OutputReference `kong:"arg"`
}

func (c *derivationInputPlaceholderCommand) Signature() string {
	return `kong:"help=Hash the placeholder for an input derivation\\'s output"`
}

func (c *derivationInputPlaceholderCommand) Run(kc *kong.Context) error {
	_, err := fmt.Fprintln(kc.Stdout, zbstore.UnknownCAOutputPlaceholder(c.OutputReference))
	return err
}

type derivationOutputPlaceholderCommand struct {
	OutputName string `kong:"arg,default=out"`
}

func (c *derivationOutputPlaceholderCommand) Signature() string {
	return `kong:"help=Hash the placeholder for an output"`
}

func (c *derivationOutputPlaceholderCommand) Run(kc *kong.Context) error {
	_, err := fmt.Fprintln(kc.Stdout, zbstore.HashPlaceholder(c.OutputName))
	return err
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
