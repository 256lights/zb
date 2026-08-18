// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"zombiezen.com/go/sqlite"
)

func TestBuffer(t *testing.T) {
	tests := []struct {
		name  string
		write func(w io.Writer) error
	}{
		{
			name: "NoWrites",
			write: func(w io.Writer) error {
				return nil
			},
		},
		{
			name: "EmptyWrite",
			write: func(w io.Writer) error {
				if n, err := w.Write(nil); err != nil {
					return err
				} else if n != 0 {
					return fmt.Errorf("no error returned, but n = %d; want 0", n)
				}
				return nil
			},
		},
		{
			name: "SingleSmall",
			write: func(w io.Writer) error {
				const p = "Hello, World!\n"
				if n, err := w.Write([]byte(p)); err != nil {
					return err
				} else if n != len(p) {
					return fmt.Errorf("no error returned, but n = %d; want %d", n, len(p))
				}
				return nil
			},
		},
		{
			name: "ByteAtATimeLarge",
			write: func(w io.Writer) error {
				const wantSize = bufferPageSize*2 + 1

				buf := [1]byte{'x'}

				for range wantSize - 1 {
					if n, err := w.Write(buf[:]); err != nil {
						return err
					} else if n != 1 {
						return fmt.Errorf("no error returned, but n = %d; want 1", n)
					}
				}
				buf[0] = '\n'
				if n, err := w.Write(buf[:]); err != nil {
					return err
				} else if n != 1 {
					return fmt.Errorf("no error returned, but n = %d; want 1", n)
				}
				return nil
			},
		},
		{
			name: "ByteWriterLarge",
			write: func(w io.Writer) error {
				const wantSize = bufferPageSize*2 + 1

				bw, isByteWriter := w.(io.ByteWriter)
				buf := [1]byte{'x'}
				write := func() error {
					if isByteWriter {
						return bw.WriteByte(buf[0])
					}
					n, err := w.Write(buf[:])
					if err != nil {
						return err
					}
					if n != 1 {
						return fmt.Errorf("no error returned, but n = %d; want 1", n)
					}
					return nil
				}

				for range wantSize - 1 {
					if err := write(); err != nil {
						return err
					}
				}
				buf[0] = '\n'
				if err := write(); err != nil {
					return err
				}
				return nil
			},
		},
		{
			name: "OneLargeWrite",
			write: func(w io.Writer) error {
				const wantSize = bufferPageSize*2 + 1
				buf := make([]byte, 0, wantSize)
				for range wantSize - 1 {
					buf = append(buf, 'x')
				}
				buf = append(buf, '\n')

				if n, err := w.Write(buf); err != nil {
					return err
				} else if n != len(buf) {
					return fmt.Errorf("no error returned, but n = %d; want %d", n, len(buf))
				}
				return nil
			},
		},
		{
			name: "ReadFromLarge",
			write: func(w io.Writer) error {
				type onlyReader struct {
					io.Reader
				}

				const wantSize = bufferPageSize*2 + 1
				buf := make([]byte, 0, wantSize)
				for range wantSize - 1 {
					buf = append(buf, 'x')
				}
				buf = append(buf, '\n')

				n, err := io.Copy(w, onlyReader{bytes.NewReader(buf)})
				if err != nil {
					return err
				} else if n != int64(len(buf)) {
					return fmt.Errorf("no error returned, but n = %d; want %d", n, len(buf))
				}
				return nil
			},
		},
		{
			name: "SmallWriteThenReadFromLarge",
			write: func(w io.Writer) error {
				type onlyReader struct {
					io.Reader
				}

				const wantSize = bufferPageSize*2 + 1
				buf := make([]byte, 0, wantSize)

				buf = append(buf, "yyy"...)
				n, err := w.Write(buf)
				if err != nil {
					return err
				} else if n != len(buf) {
					return fmt.Errorf("no error returned, but n = %d; want %d", n, len(buf))
				}

				buf = buf[:0]
				for range wantSize - 1 {
					buf = append(buf, 'x')
				}
				buf = append(buf, '\n')

				n64, err := io.Copy(w, onlyReader{bytes.NewReader(buf)})
				if err != nil {
					return err
				} else if n64 != int64(len(buf)) {
					return fmt.Errorf("no error returned, but n = %d; want %d", n64, len(buf))
				}
				return nil
			},
		},
	}

	readStrategies := []struct {
		name string
		read func(buf *buffer) ([]byte, error)
	}{
		{
			name: "Read",
			read: func(buf *buffer) ([]byte, error) {
				return io.ReadAll(buf)
			},
		},
		{
			name: "ReadByte",
			read: func(buf *buffer) ([]byte, error) {
				got := new(bytes.Buffer)
				for {
					b, err := buf.ReadByte()
					if err != nil {
						if err == io.EOF {
							err = nil
						}
						return got.Bytes(), err
					}
					got.WriteByte(b)
				}
			},
		},
		{
			name: "WriterTo",
			read: func(buf *buffer) ([]byte, error) {
				got := new(bytes.Buffer)
				n, err := io.Copy(got, buf)
				if err == nil && n != int64(got.Len()) {
					err = fmt.Errorf("io.Copy(...) = %d, <nil>; want %d, <nil>", n, got.Len())
				}
				return got.Bytes(), err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := new(bytes.Buffer)
			if err := test.write(want); err != nil {
				t.Fatal(err)
			}

			for _, strategy := range readStrategies {
				t.Run(strategy.name, func(t *testing.T) {
					conn, err := sqlite.OpenConn(":memory:", sqlite.OpenReadWrite)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := conn.Close(); err != nil {
							t.Error("conn.Close:", err)
						}
					}()

					buf, err := newBuffer(conn)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := buf.Close(); err != nil {
							t.Error("buf.Close:", err)
						}
					}()

					if err := test.write(buf); err != nil {
						t.Error("during write:", err)
					}
					if got, want := buf.Len(), int64(want.Len()); got != want {
						t.Errorf("after write, buf.Len() = %d; want %d", got, want)
					}

					got, err := strategy.read(buf)
					if err != nil {
						t.Error("during read:", err)
					}
					if !bytes.Equal(got, want.Bytes()) {
						gotPath := filepath.Join(t.ArtifactDir(), "got.out")
						os.WriteFile(gotPath, got, 0o666)
						wantPath := filepath.Join(t.ArtifactDir(), "want.out")
						os.WriteFile(wantPath, want.Bytes(), 0o666)

						t.Errorf("Buffer content does not match (%d bytes vs. %d bytes). Wrote to %s",
							len(got), want.Len(), gotPath)
					}

					if got := buf.Len(); got != 0 {
						t.Errorf("after write, buf.Len() = %d; want 0", got)
					}
				})
			}
		})
	}
}

func BenchmarkBufferRead(b *testing.B) {
	conn, err := sqlite.OpenConn(":memory:", sqlite.OpenReadWrite)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			b.Error("conn.Close:", err)
		}
	}()

	buf, err := newBuffer(conn)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := buf.Close(); err != nil {
			b.Error("buf.Close:", err)
		}
	}()

	payload := make([]byte, bufferPageSize*4)
	for i := range payload {
		payload[i] = byte(i)
	}
	b.SetBytes(int64(len(payload)))

	got := make([]byte, len(payload))

	for b.Loop() {
		b.StopTimer()
		n, err := buf.Write(payload)
		if err != nil {
			b.Fatal("buf.Write:", err)
		}
		if n != len(payload) {
			b.Fatalf("buf.Write wrote %d bytes out of %d", n, len(payload))
		}
		b.StartTimer()

		n, err = io.ReadFull(buf, got)
		if n < len(got) {
			b.Errorf("read %d bytes out of %d", n, len(got))
		}
		if err != nil {
			b.Error("read error:", err)
		}
		if b.Failed() {
			return
		}
	}
}

func BenchmarkBufferWrite(b *testing.B) {
	conn, err := sqlite.OpenConn(":memory:", sqlite.OpenReadWrite)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			b.Error("conn.Close:", err)
		}
	}()

	buf, err := newBuffer(conn)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := buf.Close(); err != nil {
			b.Error("buf.Close:", err)
		}
	}()

	payload := make([]byte, bufferPageSize*4)
	for i := range payload {
		payload[i] = byte(i)
	}
	b.SetBytes(int64(len(payload)))

	for b.Loop() {
		n, err := buf.Write(payload)
		if err != nil {
			b.Fatal("buf.Write:", err)
		}
		if n != len(payload) {
			b.Fatalf("buf.Write wrote %d bytes out of %d", n, len(payload))
		}

		b.StopTimer()
		if err := buf.Close(); err != nil {
			b.Fatal("buf.Close:", err)
		}
		b.StartTimer()
	}
}
