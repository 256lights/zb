// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

// Package zbstorerpc is the reference implementation of the [zb store RPC protocol].
//
// [zb store RPC protocol]: https://github.com/256lights/zb/blob/main/internal/zbstorerpc/README.md
package zbstorerpc

import (
	"encoding/base64"
	"fmt"
	"iter"
	"slices"
	"time"
	"unicode/utf8"

	"zb.256lights.llc/pkg/internal/xiter"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
)

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
	Expand    *ExpandResult       `json:"expand,omitempty"`
}

// Duration returns the length of the build.
func (resp *Build) Duration() time.Duration {
	if !resp.EndedAt.Valid {
		return 0
	}
	return resp.EndedAt.X.Sub(resp.StartedAt)
}

// ResultForPath returns the build result with the given derivation path.
// It returns an error if there is not exactly one.
func (resp *Build) ResultForPath(drvPath zbstore.Path) (*BuildResult, error) {
	var seq iter.Seq[*BuildResult]
	if resp == nil {
		seq = func(yield func(*BuildResult) bool) {}
	} else {
		seq = func(yield func(*BuildResult) bool) {
			for _, result := range resp.Results {
				if result.DrvPath == drvPath {
					if !yield(result) {
						return
					}
				}
			}
		}
	}

	result, err := xiter.Single(seq)
	if err != nil {
		err = fmt.Errorf("build result for %s: %w", drvPath, err)
	}
	return result, err
}

// FindRealizeOutput searches through resp.Results for the given output.
func (resp *Build) FindRealizeOutput(ref zbstore.OutputReference) (Nullable[zbstore.Path], error) {
	var results []*BuildResult
	if resp != nil {
		results = resp.Results
	}
	return FindRealizeOutput(slices.Values(results), ref)
}

// GetBuildResultMethod is the name of the method
// that queries the status of a single builder in a [Build].
// [GetBuildResultRequest] is used for the request
// and [BuildResult] is used for the response.
const GetBuildResultMethod = "zb.getBuildResult"

// GetBuildResultRequest is the set of parameters for [GetBuildMethod].
type GetBuildResultRequest struct {
	BuildID string       `json:"buildID"`
	DrvPath zbstore.Path `json:"drvPath"`
}

// BuildResult is the result of a single derivation in a [Build].
type BuildResult struct {
	DrvPath zbstore.Path     `json:"drvPath"`
	Status  BuildStatus      `json:"status"`
	Outputs []*RealizeOutput `json:"outputs"`
	LogSize int64            `json:"logSize"`
}

// OutputForName returns the [*RealizeOutput] with the given name.
// It returns an error if there is not exactly one.
func (result *BuildResult) OutputForName(name string) (*RealizeOutput, error) {
	var seq iter.Seq[*RealizeOutput]
	if result == nil {
		seq = func(yield func(*RealizeOutput) bool) {}
	} else {
		seq = func(yield func(*RealizeOutput) bool) {
			for _, out := range result.Outputs {
				if out.Name == name {
					if !yield(out) {
						return
					}
				}
			}
		}
	}

	output, err := xiter.Single(seq)
	if err != nil {
		err = fmt.Errorf("output for %s: %w", name, err)
	}
	return output, err
}

// FindRealizeOutput searches through a list of [*BuildResult] values for the given output.
func FindRealizeOutput(results iter.Seq[*BuildResult], ref zbstore.OutputReference) (Nullable[zbstore.Path], error) {
	p, err := xiter.Single(func(yield func(Nullable[zbstore.Path]) bool) {
		for result := range results {
			if result.DrvPath != ref.DrvPath {
				continue
			}
			for _, output := range result.Outputs {
				if output.Name == ref.OutputName {
					if !yield(output.Path) {
						return
					}
				}
			}
		}
	})
	if err != nil {
		return Nullable[zbstore.Path]{}, fmt.Errorf("look up %v: %w", ref, err)
	}
	return p, nil
}

// RealizeOutput is an output in [BuildResult].
type RealizeOutput struct {
	// OutputName is the name of the output that was built (e.g. "out" or "dev").
	Name string `json:"name"`
	// Path is the store path of the output if successfully built,
	// or null if the build failed.
	Path Nullable[zbstore.Path] `json:"path"`
}

// CancelBuildMethod is the name of the method that informs the store
// that the client is no longer interested in the results of the build
// and wishes it to be canceled.
// [CancelBuildNotification] is used for the request
// and the response is ignored.
const CancelBuildMethod = "zb.cancelBuild"

// CancelBuildNotification is the set of parameters for [CancelBuildMethod].
type CancelBuildNotification struct {
	BuildID string `json:"buildID"`
}

// ReadLogMethod is the name of the method that reads the build log from a running build.
// [ReadLogRequest] is used for the request
// and [ReadLogResponse] is used for the response.
const ReadLogMethod = "zb.readLog"

// ReadLogRequest is the set of parameters for [ReadLogMethod].
type ReadLogRequest struct {
	BuildID string       `json:"buildID"`
	DrvPath zbstore.Path `json:"drvPath"`
	// RangeStart is the first byte of the log to read,
	// where zero is the start of the log.
	// If RangeStart is greater than the number of bytes in the log
	// and the derivation has finished building,
	// then an error is returned.
	// If RangeStart is greater than or equal to the number of bytes in the log
	// and the derivation's build is still active,
	// then the method blocks until at least RangeStart+1 bytes have been written to the log.
	RangeStart int64 `json:"rangeStart"`
	// RangeEnd is an optional upper bound on the number of bytes to read.
	// If non-null, it must be greater than RangeStart.
	// This method may return less bytes than requested.
	RangeEnd Nullable[int64] `json:"rangeEnd"`
}

// ReadLogResponse is the result for [ReadLogMethod].
// At most one of Text or Base64 should be set;
// the payload fields can be read with [*ReadLogResponse.Payload]
// and can be written with [*ReadLogResponse.SetPayload].
type ReadLogResponse struct {
	Text   string `json:"text,omitempty"`
	Base64 string `json:"base64,omitempty"`

	// EOF indicates whether the end of this payload is the end of the log.
	// If true, this implies that the derivation has finished its realization.
	EOF bool `json:"eof"`
}

// Payload returns the log's byte content.
func (resp *ReadLogResponse) Payload() ([]byte, error) {
	switch {
	case resp.Base64 != "":
		return base64.StdEncoding.DecodeString(resp.Base64)
	case resp.Text != "":
		return []byte(resp.Text), nil
	default:
		return nil, nil
	}
}

// SetPayload sets resp.Text and resp.Base64 to reflect the given payload.
func (resp *ReadLogResponse) SetPayload(src []byte) {
	if utf8.Valid(src) {
		resp.Text = string(src)
		resp.Base64 = ""
	} else {
		resp.Text = ""
		resp.Base64 = base64.StdEncoding.EncodeToString(src)
	}
}

// ExportMethod is the name of the method that triggers an export of store objects.
// [ExportRequest] is used for the request and the response is null.
const ExportMethod = "zb.export"

// ExportIDExtraFieldName is the name of the extra field in [jsonrpc.Request]
// used to pass a value that will be passed through with [ExportIDHeaderName].
const ExportIDExtraFieldName = "zbExportID"

// ExportIDHeaderName is the name of the header used to correlate an [ExportRequest]
// with an export message.
// The request should have used [ExportIDExtraFieldName].
const ExportIDHeaderName = "Zb-Export-Id"

// ExportRequest is the set of parameters for [ExportMethod].
type ExportRequest struct {
	Paths []zbstore.Path `json:"paths"`

	// If ExcludeReferences is true, then only the paths in Paths will be exported.
	// Otherwise, paths that are referenced by those store objects will also be included.
	ExcludeReferences bool `json:"excludeReferences"`
}

// Nullable wraps a type to permit a null JSON serialization.
// The zero value is null.
type Nullable[T any] = zbstore.Nullable[T]

// NonNull returns a [Nullable] that wraps the given value.
func NonNull[T any](x T) Nullable[T] {
	return zbstore.NonNull(x)
}
