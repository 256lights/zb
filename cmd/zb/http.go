// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"

	"zb.256lights.llc/pkg/internal/fileurl"
	"zb.256lights.llc/pkg/internal/netrc"
	"zb.256lights.llc/pkg/internal/useragent"
	"zb.256lights.llc/pkg/internal/xslices"
)

type httpClient struct {
	Transport     http.RoundTripper
	FileTransport fileurl.Transport
	Netrc         []byte
}

func (c *httpClient) Do(req *http.Request) (*http.Response, error) {
	userAgentValues := req.Header.Values("User-Agent")
	username, password, shouldAddAuth := c.authorization(req)
	if len(userAgentValues) == 0 || shouldAddAuth {
		req = new(*req)
		if req.Header == nil {
			req.Header = make(http.Header)
		} else {
			req.Header = maps.Clone(req.Header)
		}
		if len(userAgentValues) == 0 {
			req.Header.Set("User-Agent", useragent.String)
		}
		if shouldAddAuth {
			req.SetBasicAuth(username, password)
		}
	}

	scheme := "http"
	if req.URL != nil {
		scheme = req.URL.Scheme
	}
	hc := &http.Client{
		Transport: c.transportFor(scheme),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if req.URL.Scheme == fileurl.Scheme && len(via) > 0 && xslices.Last(via).URL.Scheme != fileurl.Scheme {
				return fmt.Errorf("cannot redirect from %s to %s", xslices.Last(via).URL.Redacted(), req.URL.Redacted())
			}
			return nil
		},
	}
	return hc.Do(req)
}

func (c *httpClient) authorization(req *http.Request) (username, password string, rewrite bool) {
	host := cmp.Or(req.Host, req.URL.Host)
	if req.Header.Get("Authorization") != "" {
		var ok bool
		username, password, ok = req.BasicAuth()
		if !ok || password != "" {
			return username, password, false
		}
		foundPassword, _ := netrc.FindUser(c.Netrc, host, username).Password()
		if foundPassword == "" {
			return username, password, false
		}
		return username, foundPassword, true
	}
	if password, hasPassword := req.URL.User.Password(); hasPassword {
		return req.URL.User.Username(), password, false
	}
	var userinfo *url.Userinfo
	if username = req.URL.User.Username(); username != "" {
		userinfo = netrc.FindUser(c.Netrc, host, username)
	} else {
		userinfo = netrc.Find(c.Netrc, host)
	}
	if userinfo == nil {
		return req.URL.User.Username(), "", false
	}
	password, _ = userinfo.Password()
	return userinfo.Username(), password, true
}

func (c *httpClient) CloseIdleConnections() {
	cic, ok := c.transportFor("http").(interface {
		CloseIdleConnections()
	})
	if ok {
		cic.CloseIdleConnections()
	}
}

func (c *httpClient) transportFor(scheme string) http.RoundTripper {
	if scheme == fileurl.Scheme {
		return c.FileTransport
	}
	if c.Transport != nil {
		return c.Transport
	}
	return http.DefaultTransport
}

type stubRoundTripper struct {
	err error
}

func (stub stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, stub.err
}
