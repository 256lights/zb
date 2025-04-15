// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix/nar"
)

// Export exports the store objects according to the request
// in `nix-store --export` format to dst.
func (s *Server) Export(ctx context.Context, dst io.Writer, req *zbstorerpc.ExportRequest) error {
	e := zbstore.NewExporter(dst)

	var manifest []*zbstore.ExportTrailer
	var err error
	if req.ExcludeReferences {
		manifest, err = s.fetchInfoForExport(ctx, req.Paths)
	} else {
		manifest, err = s.findExportClosure(ctx, req.Paths)
	}
	if err != nil {
		return fmt.Errorf("export %s: %v", joinStrings(req.Paths, ", "), err)
	}

	for _, object := range manifest {
		if err := nar.DumpPath(e, s.realPath(object.StorePath)); err != nil {
			return fmt.Errorf("export %s: %v", object.StorePath, err)
		}
		if err := e.Trailer(object); err != nil {
			return fmt.Errorf("export %s: %v", object.StorePath, err)
		}
	}
	if err := e.Close(); err != nil {
		return fmt.Errorf("export %s: %v", joinStrings(req.Paths, ", "), err)
	}

	return nil
}

func (s *Server) export(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	conn := exporterFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("internal error: no exporter present")
	}
	args := new(zbstorerpc.ExportRequest)
	if err := json.Unmarshal(req.Params, args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}

	var header jsonrpc.Header
	if id, ok := jsonrpc.RequestIDFromContext(ctx); ok {
		s, err := marshalJSONString(id)
		if err != nil {
			log.Warnf(ctx, "Marshal request ID for export: %v", err)
		} else {
			header = make(jsonrpc.Header)
			header.Set("X-Request-Id", s)
		}
	}

	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		close(done)
		pw.CloseWithError(s.Export(ctx, pw, args))
	}()
	defer func() {
		<-done
		pr.Close()
	}()

	if err := conn.Export(header, pr); err != nil {
		return nil, err
	}
	return nil, nil
}

// fetchInfoForExport generates export trailers for the given paths.
func (s *Server) fetchInfoForExport(ctx context.Context, paths []zbstore.Path) ([]*zbstore.ExportTrailer, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	rollback, err := readonlySavepoint(conn)
	if err != nil {
		return nil, err
	}
	defer rollback()

	var result []*zbstore.ExportTrailer
	for _, path := range paths {
		info, err := pathInfo(conn, path)
		if err != nil {
			return nil, err
		}
		result = append(result, &zbstore.ExportTrailer{
			StorePath:      path,
			References:     *sets.NewSorted(info.References...),
			ContentAddress: info.CA,
		})
	}
	return result, nil
}

// findExportClosure returns a list of export trailers
// for all the store objects that are transitively referenced by the given paths.
// The list is in topological order,
// so each store object in the list will only reference itself
// or store objects that come before it in the list.
func (s *Server) findExportClosure(ctx context.Context, paths []zbstore.Path) ([]*zbstore.ExportTrailer, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	conn, err := s.db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.db.Put(conn)

	rollback, err := readonlySavepoint(conn)
	if err != nil {
		return nil, err
	}
	defer rollback()

	var result []*zbstore.ExportTrailer
	hasPath := func(s []*zbstore.ExportTrailer, path zbstore.Path) bool {
		return slices.ContainsFunc(s, func(t *zbstore.ExportTrailer) bool {
			return t.StorePath == path
		})
	}
	for _, path := range paths {
		sortEnd := len(result)

		// Gather closure.
		var infoError error
		err := closurePaths(conn, pathAndEquivalenceClass{path: path}, func(pe pathAndEquivalenceClass) bool {
			if hasPath(result, pe.path) {
				return true
			}
			var info *zbstorerpc.ObjectInfo
			info, infoError = pathInfo(conn, pe.path)
			if infoError != nil {
				return false
			}
			result = append(result, &zbstore.ExportTrailer{
				StorePath:      pe.path,
				References:     *sets.NewSorted(info.References...),
				ContentAddress: info.CA,
			})
			return true
		})
		if infoError != nil {
			return nil, infoError
		}
		if err != nil {
			return nil, err
		}

		// Topologically sort new closure.
		for ; sortEnd < len(result); sortEnd++ {
			sorted := result[:sortEnd]
			unsorted := result[sortEnd:]
			i := slices.IndexFunc(unsorted, func(t *zbstore.ExportTrailer) bool {
				for ref := range t.References.Values() {
					if ref != t.StorePath && !hasPath(sorted, ref) {
						return false
					}
				}
				return true
			})
			if i == -1 {
				return nil, fmt.Errorf("closure of %s missing referenced objects", path)
			}
			// Move object to front of unsorted slice.
			unsorted[0], unsorted[i] = unsorted[i], unsorted[0]
		}
	}

	return result, nil
}
