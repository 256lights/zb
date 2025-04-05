// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/handlers"
	"golang.org/x/sync/errgroup"
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
	var data struct {
		Query        string
		RecentBuilds []*buildResource
	}
	buildIDs, err := srv.backend.RecentBuildIDs(ctx, 25)
	if err != nil {
		return nil, err
	}

	grp, grpCtx := errgroup.WithContext(ctx)
	grp.SetLimit(3)
	data.RecentBuilds = make([]*buildResource, len(buildIDs))
	for i, id := range buildIDs {
		if grpCtx.Err() != nil {
			break
		}

		data.RecentBuilds[i] = &buildResource{
			ID: id,
			GetBuildResponse: zbstorerpc.GetBuildResponse{
				Status: zbstorerpc.BuildUnknown,
			},
		}

		grp.Go(func() error {
			req := &zbstorerpc.GetBuildRequest{
				BuildID: data.RecentBuilds[i].ID,
			}
			respPtr := &data.RecentBuilds[i].GetBuildResponse
			return jsonrpc.Do(grpCtx, srv.backend, zbstorerpc.GetBuildMethod, respPtr, req)
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

type buildResource struct {
	ID string
	zbstorerpc.GetBuildResponse
}

func (srv *webServer) build(ctx context.Context, r *http.Request) (*action.Response, error) {
	data := new(buildResource)
	data.ID = r.PathValue("id")
	err := jsonrpc.Do(ctx, srv.backend, zbstorerpc.GetBuildMethod, &data.GetBuildResponse, &zbstorerpc.GetBuildRequest{
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
