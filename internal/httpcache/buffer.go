// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"fmt"
	"io"

	"zb.256lights.llc/pkg/internal/multierror"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// bufferPageSize is the increment in which to page out potentially large responses to disk
// as they are being streamed from the origin server.
const bufferPageSize = 16 << 10 // 16 KiB

// A buffer is a variable-sized byte buffer
// backed by a fixed in-memory buffer (size 2 * [bufferPageSize])
// and a SQLite connection's [temporary database].
// Methods on a buffer can be called inside or outside of a transaction.
//
// [temporary database]: https://sqlite.org/tempfiles.html#temp_databases
type buffer struct {
	conn *sqlite.Conn

	rbuf    []byte
	rpos    int
	n       int64
	blobIDs []int64

	wbuf []byte
	werr error
}

// newBuffer returns a new [*buffer]
// that is backed by the given SQLite connection.
// The caller is responsible for calling [*buffer.Close]
// when it is no longer using the buffer.
func newBuffer(conn *sqlite.Conn) (*buffer, error) {
	stmt := conn.Prep(`create table if not exists temp."httpCacheBuffer" ("blob" blob not null);`)
	if err := runStatement(stmt); err != nil {
		return nil, fmt.Errorf("create buffer: %v", err)
	}
	buf := make([]byte, 2*bufferPageSize)
	return &buffer{
		conn: conn,
		rbuf: buf[:0:bufferPageSize],
		wbuf: buf[bufferPageSize:bufferPageSize],
	}, nil
}

// Len returns the number of unread bytes in the buffer.
func (buf *buffer) Len() int64 {
	return buf.n
}

// Write writes p to the end of the buffer.
func (buf *buffer) Write(p []byte) (n int, err error) {
	for len(p) > 0 {
		buf.flushIfFull()
		if buf.werr != nil {
			return n, buf.werr
		}
		nn := copy(buf.wbuf[len(buf.wbuf):cap(buf.wbuf)], p)
		buf.wbuf = buf.wbuf[:len(buf.wbuf)+nn]
		n += nn
		buf.n += int64(nn)
		p = p[nn:]
	}
	return n, nil
}

// WriteByte writes b to the end of the buffer.
func (buf *buffer) WriteByte(b byte) error {
	buf.flushIfFull()
	if buf.werr != nil {
		return buf.werr
	}
	buf.wbuf = append(buf.wbuf, b)
	buf.n++
	return nil
}

// ReadFrom reads data from r until [io.EOF] or error
// and writes it to the end of the buffer.
// ReadFrom returns the number of bytes read
// and any error except [io.EOF] encountered during the read.
func (buf *buffer) ReadFrom(r io.Reader) (n int64, err error) {
	for {
		buf.flushIfFull()
		if buf.werr != nil {
			return n, buf.werr
		}
		var nn int
		nn, err = r.Read(buf.wbuf[len(buf.wbuf):cap(buf.wbuf)])
		buf.wbuf = buf.wbuf[:len(buf.wbuf)+nn]
		buf.n += int64(nn)
		n += int64(nn)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return n, err
		}
	}
}

// flushIfFull copies buf.wbuf to a new blob in the temp table
// if it is at capacity.
// If any error occurs, it is stored in buf.werr.
func (buf *buffer) flushIfFull() {
	if len(buf.wbuf) < cap(buf.wbuf) || buf.werr != nil {
		return
	}

	stmt := buf.conn.Prep(`insert into temp."httpCacheBuffer" values (?);`)
	stmt.BindBytes(1, buf.wbuf)
	if err := runStatement(stmt); err != nil {
		buf.werr = fmt.Errorf("write cache buffer: %v", err)
		return
	}
	buf.blobIDs = append(buf.blobIDs, buf.conn.LastInsertRowID())
	buf.wbuf = buf.wbuf[:0]
}

// Read reads up to len(p) bytes from the beginning of the buffer into p.
func (buf *buffer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := buf.fillReadBuffer(); err != nil {
		return 0, fmt.Errorf("read cache buffer: %v", err)
	}
	n = copy(p, buf.rbuf[buf.rpos:])
	buf.rpos += n
	buf.n -= int64(n)
	if buf.rpos >= len(buf.rbuf) && len(buf.blobIDs) == 0 && len(buf.wbuf) == 0 {
		return n, io.EOF
	}
	return n, nil
}

// ReadByte reads and returns the next byte from the input or any error encountered.
func (buf *buffer) ReadByte() (byte, error) {
	if err := buf.fillReadBuffer(); err != nil {
		return 0, fmt.Errorf("read cache buffer: %v", err)
	}
	if buf.rpos >= len(buf.rbuf) {
		return 0, io.EOF
	}
	b := buf.rbuf[buf.rpos]
	buf.rpos++
	buf.n--
	return b, nil
}

// WriteTo writes the buffer to w,
// returning the number of bytes written and any error encountered.
func (buf *buffer) WriteTo(w io.Writer) (n int64, err error) {
	for {
		if err := buf.fillReadBuffer(); err != nil {
			return n, fmt.Errorf("read cache buffer: %v", err)
		}
		if buf.rpos >= len(buf.rbuf) {
			return n, nil
		}
		var nn int
		nn, err = w.Write(buf.rbuf[buf.rpos:])
		buf.n -= int64(nn)
		n += int64(nn)
		buf.rpos += nn
		if err != nil {
			return n, err
		}
	}
}

// fillReadBuffer moves the next (potentially in-progress) blob into buf.rbuf
// if there are no more bytes to read from buf.rbuf.
func (buf *buffer) fillReadBuffer() (err error) {
	if buf.rpos < len(buf.rbuf) {
		return nil
	}
	if len(buf.blobIDs) == 0 {
		n := copy(buf.rbuf[:cap(buf.rbuf)], buf.wbuf)
		buf.rbuf = buf.rbuf[:n]
		buf.rpos = 0
		buf.wbuf = append(buf.wbuf[:0], buf.wbuf[n:]...)
		return nil
	}

	defer sqlitex.Save(buf.conn)(&err)
	stmt := buf.conn.Prep(`select "blob" from temp."httpCacheBuffer" where rowid = ? limit 1;`)
	id := buf.blobIDs[0]
	stmt.BindInt64(1, id)
	hasRow, err := stmt.Step()
	if err != nil {
		stmt.Reset()
		return err
	}
	if !hasRow {
		return fmt.Errorf("missing blob rowid=%d", id)
	}
	blobSize := stmt.ColumnLen(0)
	if blobSize > cap(buf.rbuf) {
		stmt.Reset()
		return fmt.Errorf("blob rowid=%d is %d bytes (>%d-byte page size)", id, blobSize, cap(buf.rbuf))
	}
	buf.rbuf = buf.rbuf[:blobSize]
	buf.rpos = 0
	stmt.ColumnBytes(0, buf.rbuf)
	stmt.Reset()
	buf.popBlobs(1)

	return nil
}

// Close clears the buffer and releases its resources.
func (buf *buffer) Close() (err error) {
	buf.rbuf = buf.rbuf[:0]
	buf.rpos = 0
	buf.wbuf = buf.wbuf[:0]
	return buf.popBlobs(len(buf.blobIDs))
}

func (buf *buffer) popBlobs(n int) (err error) {
	if len(buf.blobIDs) == 0 {
		return nil
	}
	var ec multierror.Collector
	releaseFunc := sqlitex.Save(buf.conn)
	defer func() {
		// Always try to commit as much as possible to save storage.
		var releaseError error
		releaseFunc(&releaseError)
		if releaseError != nil {
			ec.Add(fmt.Errorf("delete cache buffer blobs: %v", releaseError))
		}
		err = ec.Error()
	}()
	stmt := buf.conn.Prep(`delete from temp."httpCacheBuffer" where rowid = ?;`)
	for _, id := range buf.blobIDs[:n] {
		stmt.BindInt64(1, id)
		if err := runStatement(stmt); err != nil {
			ec.Add(fmt.Errorf("delete cache buffer blob rowid=%d: %v", id, err))
		}
	}
	buf.blobIDs = append(buf.blobIDs[:0], buf.blobIDs[n:]...)
	return nil
}
