// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/handlers"
	"golang.org/x/sync/errgroup"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/rangeheader"
	"zb.256lights.llc/pkg/internal/xnet"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/bass/action"
	"zombiezen.com/go/log"
)

type webServer struct {
	backend       *backend.Server
	templateFiles fs.FS
	staticAssets  fs.FS
}

func (srv *webServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	cfg := &action.Config[*http.Request]{
		MaxRequestSize: 4 * 1024 * 1024,
		TemplateFiles:  srv.templateFiles,
		ReportError: func(ctx context.Context, err error) {
			log.Errorf(ctx, "%v", err)
		},
	}
	mux.Handle("/{$}", handlers.MethodHandler{
		http.MethodGet:  cfg.NewHandler(srv.home),
		http.MethodHead: cfg.NewHandler(srv.home),
	})
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(srv.staticAssets)))
	mux.Handle("/build/{$}", handlers.MethodHandler{
		http.MethodGet:  cfg.NewHandler(srv.listBuilds),
		http.MethodHead: cfg.NewHandler(srv.listBuilds),
	})
	mux.Handle("/build/{id}/{$}", handlers.MethodHandler{
		http.MethodGet:  cfg.NewHandler(srv.showBuild),
		http.MethodHead: cfg.NewHandler(srv.showBuild),
	})

	mux.Handle("/build/{id}/result", handlers.MethodHandler{
		http.MethodGet:  cfg.NewHandler(srv.showResult),
		http.MethodHead: cfg.NewHandler(srv.showResult),
	})
	mux.Handle("/build/{id}/log", handlers.MethodHandler{
		http.MethodGet:  http.HandlerFunc(srv.showLog),
		http.MethodHead: http.HandlerFunc(srv.showLog),
	})

	mux.ServeHTTP(w, r)
}

func (srv *webServer) home(ctx context.Context, r *http.Request) (*action.Response, error) {
	var data struct {
		Query        string
		RecentBuilds []*zbstorerpc.Build
	}
	buildIDs, err := srv.backend.RecentBuildIDs(ctx, 25)
	if err != nil {
		return nil, err
	}

	grp, grpCtx := errgroup.WithContext(ctx)
	grp.SetLimit(3)
	data.RecentBuilds = make([]*zbstorerpc.Build, len(buildIDs))
	for i, id := range buildIDs {
		if grpCtx.Err() != nil {
			break
		}

		data.RecentBuilds[i] = &zbstorerpc.Build{
			ID:     id,
			Status: zbstorerpc.BuildUnknown,
		}

		grp.Go(func() error {
			req := &zbstorerpc.GetBuildRequest{
				BuildID: id,
			}
			return jsonrpc.Do(grpCtx, srv.backend, zbstorerpc.GetBuildMethod, data.RecentBuilds[i], req)
		})
	}
	if err := grp.Wait(); err != nil {
		return nil, err
	}

	return &action.Response{
		HTMLTemplate: "index.html",
		TemplateData: data,
	}, nil
}

func (srv *webServer) listBuilds(ctx context.Context, r *http.Request) (*action.Response, error) {
	if buildID := strings.TrimSpace(r.FormValue("id")); buildID != "" {
		return &action.Response{
			SeeOther: "/build/" + url.PathEscape(buildID) + "/",
		}, nil
	}
	return &action.Response{SeeOther: "/"}, nil
}

func (srv *webServer) showBuild(ctx context.Context, r *http.Request) (*action.Response, error) {
	data := new(zbstorerpc.Build)
	data.ID = r.PathValue("id")
	err := jsonrpc.Do(ctx, srv.backend, zbstorerpc.GetBuildMethod, data, &zbstorerpc.GetBuildRequest{
		BuildID: data.ID,
	})
	switch {
	case err != nil:
		return nil, err
	case data.Status == zbstorerpc.BuildUnknown:
		return &action.Response{
			StatusCode:   http.StatusNotFound,
			HTMLTemplate: "build404.html",
			TemplateData: data,
		}, nil
	default:
		return &action.Response{
			HTMLTemplate: "build.html",
			TemplateData: data,
		}, nil
	}
}

func (srv *webServer) showResult(ctx context.Context, r *http.Request) (*action.Response, error) {
	var data struct {
		BuildID string
		*zbstorerpc.BuildResult
		InitialLog string
		HasMoreLog bool
	}
	data.BuildID = r.PathValue("id")
	drvPath, err := zbstore.ParsePath(r.FormValue("drvPath"))
	if data.BuildID == "" || err != nil {
		return nil, action.ErrNotFound
	}
	data.BuildResult = new(zbstorerpc.BuildResult)
	err = jsonrpc.Do(ctx, srv.backend, zbstorerpc.GetBuildResultMethod, data.BuildResult, &zbstorerpc.GetBuildResultRequest{
		BuildID: data.BuildID,
		DrvPath: drvPath,
	})
	if err != nil {
		return nil, err
	}
	if data.Status == zbstorerpc.BuildUnknown {
		return nil, action.ErrNotFound
	}

	initialLogBytes, err := readLog(ctx, srv.backend, &zbstorerpc.ReadLogRequest{
		BuildID:  data.BuildID,
		DrvPath:  drvPath,
		RangeEnd: zbstorerpc.NonNull(int64(4 * 1024)),
	})
	if err != nil && err != io.EOF {
		return nil, err
	}
	data.InitialLog = trimToUTF8(initialLogBytes)
	data.HasMoreLog = err == nil

	return &action.Response{
		HTMLTemplate: "build_result.html",
		TemplateData: data,
	}, nil
}

func (srv *webServer) showLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	buildID := r.PathValue("id")
	drvPath, err := zbstore.ParsePath(r.FormValue("drvPath"))
	if buildID == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	spec := rangeheader.StartingAt(0)
	if rangeHeader, err := rangeheader.Parse(r.Header.Get("Range")); err != nil {
		http.Error(w, "Invalid Range header: "+err.Error(), http.StatusBadRequest)
		return
	} else if len(rangeHeader) > 1 {
		http.Error(w, "Only one Range specifier permitted", http.StatusUnprocessableEntity)
		return
	} else if len(rangeHeader) == 1 {
		spec = rangeHeader[0]
	}

	result := new(zbstorerpc.BuildResult)
	err = jsonrpc.Do(ctx, srv.backend, zbstorerpc.GetBuildResultMethod, result, &zbstorerpc.GetBuildResultRequest{
		BuildID: buildID,
		DrvPath: drvPath,
	})
	if err != nil {
		log.Errorf(ctx, "%v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if result.Status == zbstorerpc.BuildUnknown {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	if !result.Status.IsFinished() && spec.IsSuffix() {
		h.Set("Content-Range", "bytes */*")
		http.Error(w, "Cannot serve suffix range on active log", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Not providing a charset because we don't necessarily know the character encoding.
	h.Set("Content-Type", "text/plain")

	h.Set("Accept-Ranges", "bytes")
	if _, hasEnd := spec.End(); spec.Start() == 0 && !hasEnd {
		if result.Status.IsFinished() {
			h.Set("Content-Length", strconv.FormatInt(result.LogSize, 10))
		}
		w.WriteHeader(http.StatusOK)
	} else {
		rangeLength := "*"
		if result.Status.IsFinished() {
			rangeLength = strconv.FormatInt(result.LogSize, 10)
		}
		var ok bool
		spec, ok = spec.Resolve(result.LogSize)
		if !ok {
			h.Set("Content-Range", "bytes */"+rangeLength)
			var msg string
			if result.Status.IsFinished() {
				msg = fmt.Sprintf("Range not satisfiable with %d bytes available in active log", result.LogSize)
			} else {
				msg = fmt.Sprintf("Range not satisfiable with log of %d bytes", result.LogSize)
			}
			http.Error(w, msg, http.StatusRequestedRangeNotSatisfiable)
			return
		}

		h.Set("Content-Range", "bytes "+spec.String()+"/"+rangeLength)
		if spec.Start() < result.LogSize {
			size, _ := spec.Size()
			h.Set("Content-Length", strconv.FormatInt(size, 10))
		} else {
			h.Set("Content-Length", "0")
		}
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method == http.MethodHead || spec.Start() >= result.LogSize {
		return
	}

	readRequest := &zbstorerpc.ReadLogRequest{
		BuildID:    buildID,
		DrvPath:    drvPath,
		RangeStart: spec.Start(),
	}
	if end, hasEnd := spec.End(); hasEnd {
		readRequest.RangeEnd = zbstorerpc.NonNull(end + 1)
	}
	for {
		payload, err := readLog(ctx, srv.backend, readRequest)
		if len(payload) > 0 {
			if _, err := w.Write(payload); err != nil {
				log.Debugf(ctx, "Read log for %s in build %s: %v", drvPath, buildID, err)
				return
			}
			readRequest.RangeStart += int64(len(payload))
		}
		if err == io.EOF || readRequest.RangeEnd.Valid && readRequest.RangeStart >= readRequest.RangeEnd.X {
			break
		}
		if err != nil {
			log.Errorf(ctx, "%v", err)
			return
		}
	}
}

func trimToUTF8(b []byte) string {
	n := len(b)
	for {
		r, size := utf8.DecodeLastRune(b[:n])
		switch {
		case size == 0:
			return ""
		case r != utf8.RuneError:
			return string(b[:n])
		}
		n -= size
	}
}

type localOnlyMiddleware struct {
	handler http.Handler
}

func (m localOnlyMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !xnet.IsLocalhost(r) {
		http.Error(w, "Only localhost connections permitted.", http.StatusForbidden)
		return
	}
	m.handler.ServeHTTP(w, r)
}
