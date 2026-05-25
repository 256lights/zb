// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package netrc

import (
	"cmp"
	"net/http"
)

// Transport is an [http.RoundTripper] that adds an Authorization header to requests
// based on credentials in a .netrc file
// before sending them to an underlying [http.RoundTripper].
type Transport struct {
	Netrc        []byte
	RoundTripper http.RoundTripper
}

// RoundTrip implements [http.RoundTripper].
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") != "" {
		username, password, ok := req.BasicAuth()
		if !ok || password != "" {
			return t.RoundTripper.RoundTrip(req)
		}
		foundPassword, _ := FindUser(t.Netrc, cmp.Or(req.Host, req.URL.Host), username).Password()
		if foundPassword == "" {
			return t.RoundTripper.RoundTrip(req)
		}
		req = new(*req)
		req.Header = req.Header.Clone()
		req.SetBasicAuth(username, foundPassword)
	} else if userinfo := Find(t.Netrc, cmp.Or(req.Host, req.URL.Host)); userinfo != nil {
		req = new(*req)
		req.Header = req.Header.Clone()
		password, _ := userinfo.Password()
		req.SetBasicAuth(userinfo.Username(), password)
	}
	return t.RoundTripper.RoundTrip(req)
}

// CloseIdleConnections calls t.RoundTripper.CloseIdleConnections(), if present.
func (t *Transport) CloseIdleConnections() {
	cic, ok := t.RoundTripper.(interface {
		CloseIdleConnections()
	})
	if ok {
		cic.CloseIdleConnections()
	}
}
