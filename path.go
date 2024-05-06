package zb

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/zb/internal/lua"
)

func (eval *Eval) pathFunction(l *lua.State) (int, error) {
	var p string
	var name string
	switch l.Type(1) {
	case lua.TypeString:
		p, _ = l.ToString(1)
	case lua.TypeTable:
		typ, err := l.Field(1, "path", 0)
		if err != nil {
			return 0, fmt.Errorf("path: %v", err)
		}
		if typ == lua.TypeNil {
			return 0, lua.NewArgError(l, 1, "missing path")
		}
		p, err = lua.ToString(l, -1)
		if err != nil {
			return 0, fmt.Errorf("path: %v", err)
		}
		l.Pop(1)

		typ, err = l.Field(1, "name", 0)
		if err != nil {
			return 0, fmt.Errorf("path: %v", err)
		}
		if typ != lua.TypeNil {
			name, _ = lua.ToString(l, -1)
		}
		l.Pop(1)
	default:
		return 0, lua.NewTypeError(l, 1, "string or table")
	}

	if !filepath.IsAbs(p) {
		// TODO(maybe): This is probably wonky with tail calls.
		debugInfo := l.Stack(1).Info("S")
		if debugInfo == nil {
			return 0, fmt.Errorf("path: no caller information available")
		}
		if source, ok := strings.CutPrefix(debugInfo.Source, "@"); ok {
			p = filepath.Join(filepath.Dir(source), filepath.FromSlash(p))
		} else {
			var err error
			p, err = filepath.Abs(filepath.FromSlash(p))
			if err != nil {
				return 0, fmt.Errorf("path: %w", err)
			}
		}
	}
	if name == "" {
		name = filepath.Base(p)
	}

	c := exec.Command("nix-store", "--import")
	c.Stderr = os.Stderr
	stdin, err := c.StdinPipe()
	if err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	if err := c.Start(); err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	_, err = stdin.Write([]byte{
		1, 0, 0, 0, 0, 0, 0, 0,
	})
	if err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}

	h := nix.NewHasher(nix.SHA256)
	if err := nar.DumpPath(io.MultiWriter(h, stdin), p); err != nil {
		stdin.Close()
		c.Process.Kill()
		c.Wait()
		return 0, fmt.Errorf("path: %w", err)
	}
	sum := h.SumHash()
	storePath, err := fixedCAOutputPath(eval.storeDir, name, nix.RecursiveFileContentAddress(sum), storeReferences{})
	if err != nil {
		stdin.Close()
		c.Process.Kill()
		c.Wait()
		return 0, fmt.Errorf("path: %w", err)
	}

	trailer := []byte{
		'N', 'I', 'X', 'E', 0, 0, 0, 0,
	}
	trailer = binary.LittleEndian.AppendUint64(trailer, uint64(len(storePath)))
	trailer = append(trailer, storePath...)
	if off := len(storePath) % 8; off != 0 {
		for i := 0; i < 8-off; i++ {
			trailer = append(trailer, 0)
		}
	}
	trailer = binary.LittleEndian.AppendUint64(trailer, 0) // number of references
	trailer = binary.LittleEndian.AppendUint64(trailer, 0) // deriver string length
	trailer = binary.LittleEndian.AppendUint64(trailer, 0) // end of object
	trailer = binary.LittleEndian.AppendUint64(trailer, 0) // end of stream
	if _, err := stdin.Write(trailer); err != nil {
		stdin.Close()
		c.Process.Kill()
		c.Wait()
		return 0, fmt.Errorf("path: %w", err)
	}
	stdin.Close()
	if err := c.Wait(); err != nil {
		return 0, fmt.Errorf("path: %w", err)
	}
	l.PushStringContext(string(storePath), []string{string(storePath)})
	return 1, nil
}
