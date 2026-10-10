package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"os"

	"zombiezen.com/go/nix"
)

type hashCommand struct {
	File    hashFileCommand    `kong:"cmd"`
}

func (c *hashCommand) Signature() string {
	return `help:"Compute and convert cryptographic hashes"`
}

type hashFileCommand struct {
	Algo     string `enum:"md5,sha1,sha256,sha512" default:"sha256" help:"Hash algorithm (${enum})"`
	Encoding string `enum:"base16,base32,base64" default:"base16" help:"Print the hash in selected format (${enum})"`

	Path string `kong:"arg"`
}

func (hf *hashFileCommand) Run() error {
	content, err := os.ReadFile(hf.Path)
	if err != nil {
		return err
	}

	var hash nix.Hash

	switch hf.Algo {
	case "md5":
		hash = nix.NewHash(nix.MD5, new(md5.Sum(content))[:])
	case "sha1":
		hash = nix.NewHash(nix.SHA1, new(sha1.Sum(content))[:])
	case "sha256":
		hash = nix.NewHash(nix.SHA256, new(sha256.Sum256(content))[:])
	case "sha512":
		hash = nix.NewHash(nix.SHA512, new(sha512.Sum512(content))[:])
	}

	switch hf.Encoding {
	case "base16":
		fmt.Printf("%s\n", hash.RawBase16())
	case "base32":
		fmt.Printf("%s\n", hash.RawBase32())
	case "base64":
		fmt.Printf("%s\n", hash.RawBase64())
	}

	return nil
}
