// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

//go:embed sql
var rawSQLFiles embed.FS

var sqlFiles = sync.OnceValue(func() fs.FS {
	fsys, err := fs.Sub(rawSQLFiles, "sql")
	if err != nil {
		panic(err)
	}
	return fsys
})

var schema = sync.OnceValue(func() sqlitemigration.Schema {
	var schema sqlitemigration.Schema
	for i := 1; ; i++ {
		migration, err := fs.ReadFile(sqlFiles(), fmt.Sprintf("schema/%02d.sql", i))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read migrations: %v", err))
		}
		schema.Migrations = append(schema.Migrations, string(migration))
	}
	return schema
})

func prepareConn(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode=wal;", nil); err != nil {
		return fmt.Errorf("enable write-ahead logging: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA synchronous=normal;", nil); err != nil {
		return fmt.Errorf("enable write-ahead logging: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys=on;", nil); err != nil {
		return fmt.Errorf("enable foreign keys: %v", err)
	}

	err := conn.SetCollation("headerkey", func(a, b string) int {
		return cmp.Compare(http.CanonicalHeaderKey(a), http.CanonicalHeaderKey(b))
	})
	if err != nil {
		return err
	}

	err = conn.CreateFunction("httpdate", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		AllowIndirect: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			arg := args[0]
			if arg.Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			t, err := http.ParseTime(arg.Text())
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.IntegerValue(t.Unix()), nil
		},
	})
	if err != nil {
		return err
	}

	return nil
}

var queryCache struct {
	mu    sync.RWMutex
	files map[string]string
}

func prepareQuery(conn *sqlite.Conn, name string) *sqlite.Stmt {
	queryCache.mu.RLock()
	query := queryCache.files[name]
	queryCache.mu.RUnlock()
	if query != "" {
		return conn.Prep(query)
	}

	query, err := readFileString(sqlFiles(), name)
	if err != nil {
		panic(err)
	}
	query = strings.TrimRight(query, "\n")

	queryCache.mu.Lock()
	if queryCache.files == nil {
		queryCache.files = make(map[string]string)
	}
	queryCache.files[name] = query
	queryCache.mu.Unlock()

	return conn.Prep(query)
}

func runStatement(stmt *sqlite.Stmt) error {
	hasRow, stepError := stmt.Step()
	resetError := stmt.Reset()
	if stepError != nil {
		return stepError
	}
	if hasRow {
		return errors.New("unexpected result row")
	}
	return resetError
}

func readFileString(fsys fs.FS, name string) (string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sb := new(strings.Builder)
	_, err = io.Copy(sb, f)
	return sb.String(), err
}
