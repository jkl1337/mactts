package main

import (
	"io"
	"net/http"
	"strconv"
)

// ResponseBuffer provides an in-memory content buffer for HTTP requests while also implementing the
// ReaderAt and WriterAt interfaces in addition to the http.ResponseWriter interface.
// ReaderSeeker is implemented to allow byte serving.
type ResponseBuffer struct {
	buf    []byte
	readOfs int
	status int
	header http.Header
}

func (rb *ResponseBuffer) Read(data []byte) (n int, err error) {
	n = copy(data, rb.buf[rb.readOfs:])
	rb.readOfs += n
	if rb.readOfs >= len(rb.buf) {
		err = io.EOF
	}
	return
}

func (rb *ResponseBuffer) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		rb.readOfs = int(offset)
	case 1:
		rb.readOfs += int(offset)
	case 2:
		rb.readOfs = len(rb.buf) + int(offset)
	}
	if rb.readOfs < 0 {
		rb.readOfs = 0
	} else if rb.readOfs > len(rb.buf) {
		rb.readOfs = len(rb.buf)
	}
	return int64(rb.readOfs), nil
}

// grow grows the buffer to guarantee space for n more bytes, increasing the length to accomdate them
func (rb *ResponseBuffer) grow(n int) int {
	m := len(rb.buf)
	if m+n > cap(rb.buf) {
		var buf []byte
		buf = make([]byte, 2*cap(rb.buf)+n)
		copy(buf, rb.buf)
		rb.buf = buf
	}
	rb.buf = rb.buf[0 : m+n]
	return m
}

func (rb *ResponseBuffer) WriteHeader(status int) {
	rb.status = status
}

func (rb *ResponseBuffer) Header() http.Header {
	if rb.header == nil {
		rb.header = make(http.Header)
	}
	return rb.header
}

// CopyHeaders copies all the headers and Content-Length for this response buffer to target http.ResponseWriter
func (rb *ResponseBuffer) CopyHeaders(w http.ResponseWriter) {
	for k, v := range rb.header {
		w.Header()[k] = v
	}
	if len(rb.buf) > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(rb.buf)))
	}
	if rb.status != 0 {
		w.WriteHeader(rb.status)
	}
}

// WriteTo writes the buffered contents and all http header information to another http.ResponseWriter.
func (rb *ResponseBuffer) WriteTo(w http.ResponseWriter) error {
	rb.CopyHeaders(w)
	if len(rb.buf) > 0 {
		if _, err := w.Write(rb.buf); err != nil {
			return err
		}
	}
	return nil
}

func (rb *ResponseBuffer) Write(p []byte) (int, error) {
	m := rb.grow(len(p))
	return copy(rb.buf[m:], p), nil
}

func (rb *ResponseBuffer) WriteAt(p []byte, off int64) (n int, err error) {
	need := len(p) + int(off) - len(rb.buf)
	if need > 0 {
		rb.grow(need)
	}
	return copy(rb.buf[off:], p), nil
}

func (rb *ResponseBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	if int(off) >= len(rb.buf) {
		if len(p) == 0 {
			return
		}
		return 0, io.EOF
	}
	n = copy(p, rb.buf[off:])
	return
}
