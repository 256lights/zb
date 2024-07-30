// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package zbstore provides the JSON-RPC types for the zb store API.
package zbstore

// ExistsMethod is the name of the method that checks whether a store path exists.
// [ExistsRequest] is used for the request and the response is a boolean.
const ExistsMethod = "zb.exists"

// ExistsRequest is the set of parameters for [ExistsMethod].
type ExistsRequest struct {
	Path string `json:"path"`
}

// RealizeMethod is the name of the method that triggers a build of a store path.
// [RealizeRequest] is used for the request
// and [RealizeResponse] is used for the response.
const RealizeMethod = "zb.realize"

// RealizeRequest is the set of parameters for [RealizeMethod].
type RealizeRequest struct {
	DrvPath Path `json:"drvPath"`
}

// RealizeResponse is the result for [RealizeMethod].
type RealizeResponse struct {
	Outputs []*RealizeOutput `json:"outputs"`
}

// RealizeOutput is an output in [RealizeResponse].
type RealizeOutput struct {
	// OutputName is the name of the output that was built (e.g. "out" or "dev").
	Name string `json:"name"`
	// Path is the store path of the output if successfully built,
	// or null if the build failed.
	Path *Path `json:"path"`
}
