// Package streamcheck wraps a stream being written to storage so that its size
// and checksums can be verified before the write is committed.
package streamcheck

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"

	"github.com/ninjamarcus/ninjaStorage/models"
)

// Reader hashes everything read through it and enforces the size limits in
// its options.
//
// Once ctx is done, Read returns the context's error straight away, even if a
// read from the underlying reader is blocked: that read is left to finish in
// the background and its data is discarded. After any error, every later Read
// returns the same error.
type Reader struct {
	ctx      context.Context
	r        io.Reader
	opts     models.WriteStreamOptions
	expected []byte
	sha      hash.Hash
	md5      hash.Hash
	n        int64
	eof      bool
	err      error
	buf      []byte
}

// New validates opts and returns a Reader over r.
func New(ctx context.Context, r io.Reader, opts models.WriteStreamOptions) (*Reader, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: reader is nil", models.ErrInvalidOptions)
	}
	if opts.ExpectedSize < 0 || opts.MaxSize < 0 || opts.ChunkSize < 0 {
		return nil, fmt.Errorf("%w: sizes cannot be negative", models.ErrInvalidOptions)
	}
	if opts.ExpectedSize > 0 && opts.MaxSize > 0 && opts.ExpectedSize > opts.MaxSize {
		return nil, fmt.Errorf("%w: expected size %d exceeds max size %d", models.ErrInvalidOptions, opts.ExpectedSize, opts.MaxSize)
	}
	var expected []byte
	if opts.ExpectedSHA256 != "" {
		b, err := hex.DecodeString(strings.ToLower(opts.ExpectedSHA256))
		if err != nil || len(b) != sha256.Size {
			return nil, fmt.Errorf("%w: expected SHA-256 must be %d hex characters", models.ErrInvalidOptions, sha256.Size*2)
		}
		expected = b
	}
	return &Reader{
		ctx:      ctx,
		r:        r,
		opts:     opts,
		expected: expected,
		sha:      sha256.New(),
		md5:      md5.New(),
	}, nil
}

// Read implements io.Reader. Bytes that would take the stream over a limit are
// never returned.
func (c *Reader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	if err := c.ctx.Err(); err != nil {
		c.err = err
		return 0, err
	}
	n, err := c.read(p)
	if n > 0 {
		total := c.n + int64(n)
		if c.opts.MaxSize > 0 && total > c.opts.MaxSize {
			c.err = models.ErrTooLarge
			return 0, c.err
		}
		if c.opts.ExpectedSize > 0 && total > c.opts.ExpectedSize {
			c.err = fmt.Errorf("%w: stream is longer than the expected %d bytes", models.ErrSizeMismatch, c.opts.ExpectedSize)
			return 0, c.err
		}
		c.n = total
		c.sha.Write(p[:n])
		c.md5.Write(p[:n])
	}
	switch {
	case err == io.EOF:
		c.eof = true
	case err != nil:
		c.err = err
	}
	return n, err
}

type readResult struct {
	n   int
	err error
}

// read reads from the underlying reader, giving up when ctx is done. The read
// itself runs in its own goroutine, into a buffer only that goroutine touches
// until it finishes, so an abandoned read can never write into p.
func (c *Reader) read(p []byte) (int, error) {
	if c.ctx.Done() == nil {
		return c.r.Read(p)
	}
	if len(c.buf) < len(p) {
		c.buf = make([]byte, len(p))
	}
	buf := c.buf[:len(p)]
	done := make(chan readResult, 1)
	go func() {
		n, err := c.r.Read(buf)
		done <- readResult{n, err}
	}()
	select {
	case res := <-done:
		copy(p, buf[:res.n])
		return res.n, res.err
	case <-c.ctx.Done():
		c.buf = nil // still owned by the abandoned read
		return 0, c.ctx.Err()
	}
}

// Verify reports whether the stream satisfies the options. Call it once Read
// has returned io.EOF and before committing the write.
func (c *Reader) Verify() error {
	if !c.eof {
		return errors.New("stream was not read to the end")
	}
	if c.opts.ExpectedSize > 0 && c.n != c.opts.ExpectedSize {
		return fmt.Errorf("%w: got %d bytes, expected %d", models.ErrSizeMismatch, c.n, c.opts.ExpectedSize)
	}
	if c.expected != nil && subtle.ConstantTimeCompare(c.sha.Sum(nil), c.expected) != 1 {
		return models.ErrChecksumMismatch
	}
	return nil
}

// Size is the number of bytes returned by Read so far.
func (c *Reader) Size() int64 { return c.n }

// SHA256 is the hex-encoded SHA-256 of the bytes returned by Read so far.
func (c *Reader) SHA256() string { return hex.EncodeToString(c.sha.Sum(nil)) }

// MD5 is the hex-encoded MD5 of the bytes returned by Read so far.
func (c *Reader) MD5() string { return hex.EncodeToString(c.md5.Sum(nil)) }
