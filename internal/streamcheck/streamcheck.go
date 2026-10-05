// Package streamcheck wraps a stream being written to storage so that its size
// and checksums can be verified before the write is committed.
package streamcheck

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"strings"

	"github.com/ninjamarcus/ninjaStorage/models"
)

// Reader hashes everything read through it and enforces the size limits in
// its options. It stops with the context's error once the context is done.
type Reader struct {
	ctx      context.Context
	r        io.Reader
	opts     models.WriteStreamOptions
	expected []byte
	sha      hash.Hash
	md5      hash.Hash
	n        int64
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

// Read implements io.Reader.
func (c *Reader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.n += int64(n)
		if c.opts.MaxSize > 0 && c.n > c.opts.MaxSize {
			return 0, models.ErrTooLarge
		}
		if c.opts.ExpectedSize > 0 && c.n > c.opts.ExpectedSize {
			return 0, models.ErrSizeMismatch
		}
		c.sha.Write(p[:n])
		c.md5.Write(p[:n])
	}
	return n, err
}

// Verify reports whether the stream read so far satisfies the options. Call it
// once the stream has been read to the end and before committing the write.
func (c *Reader) Verify() error {
	if c.opts.ExpectedSize > 0 && c.n != c.opts.ExpectedSize {
		return fmt.Errorf("%w: got %d bytes, expected %d", models.ErrSizeMismatch, c.n, c.opts.ExpectedSize)
	}
	if c.expected != nil && subtle.ConstantTimeCompare(c.sha.Sum(nil), c.expected) != 1 {
		return models.ErrChecksumMismatch
	}
	return nil
}

// Size is the number of bytes read so far.
func (c *Reader) Size() int64 { return c.n }

// SHA256 is the hex-encoded SHA-256 of the bytes read so far.
func (c *Reader) SHA256() string { return hex.EncodeToString(c.sha.Sum(nil)) }

// MD5 is the hex-encoded MD5 of the bytes read so far.
func (c *Reader) MD5() string { return hex.EncodeToString(c.md5.Sum(nil)) }
