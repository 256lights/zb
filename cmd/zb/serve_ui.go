// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/handlers"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/xnet"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
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
		http.MethodGet:  cfg.NewHandler(srv.build),
		http.MethodHead: cfg.NewHandler(srv.build),
	})

	mux.ServeHTTP(w, r)
}

func (srv *webServer) home(ctx context.Context, r *http.Request) (*action.Response, error) {
	return &action.Response{
		HTMLTemplate: "index.html",
	}, nil
}

func (srv *webServer) listBuilds(ctx context.Context, r *http.Request) (*action.Response, error) {
	if buildID := strings.TrimSpace(r.FormValue("id")); buildID != "" {
		return &action.Response{
			SeeOther: "/build/" + url.PathEscape(buildID) + "/",
		}, nil
	}

	return nil, action.WithStatusCode(http.StatusNotFound, fmt.Errorf("TODO(soon)"))
}

func (srv *webServer) build(ctx context.Context, r *http.Request) (*action.Response, error) {
	var data struct {
		ID string
		*zbstorerpc.GetBuildResponse
	}
	data.ID = r.PathValue("id")
	data.GetBuildResponse = new(zbstorerpc.GetBuildResponse)
	err := jsonrpc.Do(ctx, srv.backend, zbstorerpc.GetBuildMethod, data.GetBuildResponse, &zbstorerpc.GetBuildRequest{
		BuildID: data.ID,
	})
	if err != nil {
		return nil, err
	}
	if data.Status == zbstorerpc.BuildUnknown {
		return nil, action.WithStatusCode(http.StatusNotFound, fmt.Errorf("build %s not found", data.ID))
	}

	return &action.Response{
		HTMLTemplate: "build.html",
		TemplateData: data,
	}, nil
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
