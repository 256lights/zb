/*
SPDX-License-Identifier: Apache-2.0

Copyright 2011, 2013 The Perkeep Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package xnet

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
)

// Adapted from https://github.com/perkeep/perkeep/blob/7ea45b9ee3bf56c5fb7e3e06e6d9f9e2cd4986a4/internal/httputil/auth.go#L34-L45:

// IsLocalhost reports whether the incoming request is from the same machine.
func IsLocalhost(r *http.Request) bool {
	remote, err := HostPortToIP(r.RemoteAddr, netip.Addr{})
	if err != nil {
		return false
	}
	host, err := HostPortToIP(r.Host, remote.Addr())
	if err != nil {
		return false
	}
	return remote.Addr().IsLoopback() && host.Addr().IsLoopback()
}

// Adapted from https://github.com/perkeep/perkeep/blob/7ea45b9ee3bf56c5fb7e3e06e6d9f9e2cd4986a4/internal/netutil/ident.go#L49-L72:

// HostPortToIP parses a host:port to an address without resolving names.
// If given a context IP, it will resolve localhost to match the context's IP family.
func HostPortToIP(hostport string, ctx netip.Addr) (hostaddr netip.AddrPort, err error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return netip.AddrPort{}, err
	}
	iport, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid port %s", port)
	}
	var addr netip.Addr
	if ctx.IsValid() && host == "localhost" {
		if ctx.Is4() {
			addr = netip.AddrFrom4([4]byte{127, 0, 0, 1})
		} else {
			addr = netip.IPv6Loopback()
		}
	} else if addr, err = netip.ParseAddr(host); err != nil {
		return netip.AddrPort{}, err
	}
	return netip.AddrPortFrom(addr, uint16(iport)), nil
}
