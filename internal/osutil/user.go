// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package osutil

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"slices"
	"strings"

	"zombiezen.com/go/log"
)

// IsRoot reports whether the process is running as the Unix root user.
func IsRoot() bool {
	return runtime.GOOS != "windows" && os.Geteuid() == rootUID
}

// LookupGroup looks up a group by name,
// including all known members of the group.
// If the group cannot be found, the returned error is of type [user.UnknownGroupError].
func LookupGroup(ctx context.Context, name string) (g *user.Group, userNames []string, err error) {
	if runtime.GOOS == "windows" {
		g, err = user.LookupGroup(name)
		return g, nil, err
	}
	groupData, err := readGroup(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	for line := range splitLines(groupData) {
		lineName, _, _ := cutByte(line, ':')
		if string(lineName) == name {
			g, userNames = parseGroup(string(line))

			// Augment with users that have the group as their primary group.
			// If this fails for some reason,
			// ignore the error, since we've already obtained a list.
			if passwd, closePasswd, err := readPasswd(ctx); err == nil {
				defer closePasswd()
				for s := bufio.NewScanner(passwd); s.Scan(); {
					username, userGID := parseUser(s.Bytes())
					if string(userGID) == g.Gid {
						if s := string(username); !slices.Contains(userNames, s) {
							userNames = append(userNames, s)
						}
					}
				}
			}

			return g, userNames, nil
		}
	}
	return nil, nil, user.UnknownGroupError(name)
}

func readPasswd(ctx context.Context) (io.Reader, func(), error) {
	// First, try getent if available. This supports multiple backends (e.g. LDAP).
	if getentPath, err := exec.LookPath("getent"); err != nil {
		log.Debugf(ctx, "Could not find getent: %v", err)
	} else {
		c := exec.CommandContext(ctx, getentPath, "--", "passwd")
		stdout, err := c.StdoutPipe()
		if err != nil {
			return nil, nil, fmt.Errorf("getent: %w", err)
		}
		if err := c.Start(); err != nil {
			return nil, nil, err
		}
		return stdout, func() {
			stdout.Close()
			c.Wait()
		}, nil
	}
	// Fall back to /etc/passwd.
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

func parseUser(line []byte) (name, gid []byte) {
	fields := bytes.SplitN(line, []byte(":"), 5)
	name = fields[0]
	if len(fields) >= 4 {
		gid = fields[3]
	}
	return
}

func readGroup(ctx context.Context, name string) ([]byte, error) {
	// First, try getent if available. This supports multiple backends (e.g. LDAP).
	if getentPath, err := exec.LookPath("getent"); err != nil {
		log.Debugf(ctx, "Could not find getent: %v", err)
	} else {
		c := exec.CommandContext(ctx, getentPath, "--", "group", name)
		data, err := c.Output()
		if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 2 {
			return nil, user.UnknownGroupError(name)
		}
		return data, err
	}

	// Fall back to /etc/group.
	return os.ReadFile("/etc/group")
}

func parseGroup(line string) (*user.Group, []string) {
	fields := strings.SplitN(line, ":", 4)
	g := &user.Group{Name: fields[0]}
	if len(fields) < 3 {
		return g, nil
	}
	g.Gid = fields[2]
	if len(fields) < 4 || fields[3] == "" {
		return g, nil
	}
	return g, strings.Split(fields[3], ",")
}

func splitLines(s []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for ss := s; len(ss) > 0; {
			line, tail, _ := cutByte(ss, '\n')
			if !yield(line) {
				return
			}
			ss = tail
		}
	}
}

func cutByte(s []byte, sep byte) (before, after []byte, found bool) {
	if i := bytes.IndexByte(s, sep); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, nil, false
}
