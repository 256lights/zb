// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package zbstorerpc is the reference implementation of the [zb store RPC protocol].
//
// [zb store RPC protocol]: https://github.com/256lights/zb/blob/main/internal/zbstorerpc/README.md
package zbstorerpc

import (
	"encoding/base64"
	"errors"
	"fmt"
	"iter"
	"net"
	"os"
	"slices"
	"time"
	"unicode/utf8"

	"zb.256lights.llc/pkg/internal/storepath"
	"zb.256lights.llc/pkg/internal/xiter"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
)

// NewListener creates a net.Listener for the zbstore RPC server.
// It listens on the Unix domain socket specified by `storepath.SocketPath()`.
// This function performs a pre-flight check for stale or active socket files
// to ensure only one server instance runs at a time and to clean up gracefully.
//
// The caller is responsible for serving RPCs on the returned listener and closing it.
// When the listener is closed, the underlying socket file will typically be removed
// by the operating system.
func NewListener() (net.Listener, error) {
	socketPath := storepath.SocketPath()

	// Check if the socket path already exists.
	fileInfo, err := os.Stat(socketPath)
	if err == nil {
		// Path exists. Check if it's an active socket, a stale one, or something else.
		conn, dialErr := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if dialErr == nil {
			// A server is actively listening on the socket.
			conn.Close()
			return nil, fmt.Errorf("zbstore: server already running; a process is listening on %q", socketPath)
		}

		// Dial failed, so the socket is likely stale.
		// Verify it's a socket file before removing it to avoid deleting other file types.
		if fileInfo.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("zbstore: path %q exists but is not a socket (mode %s); please remove it manually", socketPath, fileInfo.Mode())
		}

		// It's a stale socket file. Attempt to remove it.
		fmt.Printf("zbstore: removing stale socket file at %q\n", socketPath)
		if removeErr := os.Remove(socketPath); removeErr != nil {
			return nil, fmt.Errorf("zbstore: could not clean up stale socket at %q: %w; please remove it manually", socketPath, removeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		// An unexpected error occurred while checking the path (e.g., permissions issue).
		return nil, fmt.Errorf("zbstore: failed to check socket path %q: %w", socketPath, err)
	}

	// At this point, either the socket path did not exist or we have successfully cleaned it up.
	// Proceed with creating the new listener.
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("zbstore: failed to listen on %q: %w", socketPath, err)
	}
	return ln, nil
}

// NopMethod is the name of the method that does nothing.
// The request is ignored and the response is null.
const NopMethod = "zb.nop"

// ExistsMethod is the name of the method that checks whether a store path exists.
// [ExistsRequest] is used for the request and the response is a boolean.
const ExistsMethod = "zb.exists"

// ExistsRequest is the set of parameters for [ExistsMethod].
type ExistsRequest struct {
	Path string `json:"path"`
}

// InfoMethod is the name of the method that returns information about a store object.
// [InfoRequest] is used for the request
// and [InfoResponse] is used for the response.
const InfoMethod = "zb.info"

// InfoRequest is the set of parameters for [InfoMethod].
type InfoRequest struct {
	Path zbstore.Path `json:"path"`
}

// InfoResponse is the result for [InfoMethod].
type InfoResponse struct {
	// Info is the information for the requested path,
	// or null if the path does not exist.
	Info *ObjectInfo `json:"info"`
}

// ObjectInfo is a condensed version of NAR info used in [InfoResponse].
type ObjectInfo struct {
	// NARHash is the hash of the decompressed .nar file.
	// Nix requires this field to be set.
	NARHash nix.Hash `json:"narHash"`
	// NARSize is the size of the decompressed .nar file in bytes.
	// Nix requires this field to be set.
	NARSize int64 `json:"narSize"`
	// References is the set of other store objects that this store object references.
	References []zbstore.Path `json:"references"`
	// CA is a content-addressability assertion.
	CA zbstore.ContentAddress `json:"ca"`
}

// RealizeMethod is the name of the method that triggers a build of a store path.
// [RealizeRequest] is used for the request
// and [RealizeResponse] is used for the response.
const RealizeMethod = "zb.realize"

// RealizeRequest is the set of parameters for [RealizeMethod].
type RealizeRequest struct {
	DrvPaths []zbstore.Path `json:"drvPath"`
	// KeepFailed indicates that if the realization fails,
	// the user wants the store to keep the build directory for further investigation.
	KeepFailed bool `json:"keepFailed"`
	// Reuse defines the set of realizations that the server can use from previous builds.
	Reuse *ReusePolicy `json:"reuse"`
}

// ReusePolicy specifies a policy for [RealizeRequest] or [ExpandRequest]
// that determine which existing realizations can be reused.
// The zero value or nil represents a policy that prevents any previous realizations from being used,
// thus performing a clean build.
type ReusePolicy struct {
	// If All is true, all realizations can be reused
	// and all other fields are ignored.
	All bool `json:"all,omitzero"`
	// PublicKeys is a set of public keys.
	// If a realization has a valid signature whose public key is in the PublicKeys set,
	// then the server will use that realization.
	PublicKeys []*zbstore.RealizationPublicKey `json:"publicKeys,omitempty"`
}

// RealizeResponse is the result for [RealizeMethod].
type RealizeResponse struct {
	BuildID string `json:"buildID"`
}

// ExpandMethod is the name of the method that performs placeholder expansion
// on a derivation.
// This will cause its dependencies to be realized,
// but the derivation itself will not.
// [ExpandRequest] is used for the request
// and [ExpandResponse] is used for the response.
const ExpandMethod = "zb.expand"

// ExpandRequest is the set of parameters for [ExpandMethod].
type ExpandRequest struct {
	DrvPath            zbstore.Path `json:"drvPath"`
	TemporaryDirectory string       `json:"tempDir"`

	// Reuse defines the set of realizations that the server can use from previous builds.
	Reuse *ReusePolicy `json:"reuse"`
}

// ExpandResponse is the result for [ExpandMethod].
type ExpandResponse struct {
	BuildID string `json:"buildID"`
}

// ExpandResult is the result in a [Build] for a build started by [ExpandMethod].
type ExpandResult struct {
	Builder string            `json:"builder"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// GetBuildMethod is the name of the method that queries the status of a build.
// [GetBuildRequest] is used for the request
// and [Build] is used for the response.
const GetBuildMethod = "zb.getBuild"

// GetBuildRequest is the set of parameters for [GetBuildMethod].
type GetBuildRequest struct {
	BuildID string `json:"buildID"`
}

// BuildStatus is an enumeration of build states in [Build].
type BuildStatus string

// Defined build states.
const (
	// BuildUnknown is the status used for a build that the store doesn't know about.
	BuildUnknown BuildStatus = "unknown"
	// BuildActive is the status used for a build in progress.
	BuildActive BuildStatus = "active"
	// BuildSuccess is the status used for a build that encountered no errors.
	BuildSuccess BuildStatus = "success"
	// BuildFail is the status used for a build that has one or more derivations that failed.
	BuildFail BuildStatus = "fail"
	// BuildError is the status used for a build that encountered an internal error.
	BuildError BuildStatus = "error"
)

// IsFinished reports whether the status indicates that the build has finished.
func (status BuildStatus) IsFinished() bool {
	return status == BuildSuccess ||
		status == BuildFail ||
		status == BuildError
}

// Build is the result for [GetBuildMethod].
type Build struct {
	ID        string              `json:"id"`
	Status    BuildStatus         `json:"status"`
	StartedAt time.Time           `json:"startedAt"`
	EndedAt   Nullable[time.Time] `json:"endedAt"`
	Results   []*BuildResult      `json:"results"`
	Expand    *ExpandResult