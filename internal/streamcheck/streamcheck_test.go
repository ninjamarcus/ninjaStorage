package streamcheck

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ninjamarcus/ninjaStorage/models"
)

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestReaderHashesAndPassesThrough(t *testing.T) {
	data := bytes.Repeat([]byte("backup"), 10000)
	c, err := New(context.Background(), bytes.NewReader(data), models.WriteStreamOptions{
		ExpectedSHA256: strings.ToUpper(sha(data)),
		ExpectedSize:   int64(len(data)),
		MaxSize:        int64(len(data)),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("data changed in transit")
	}
	if err := c.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Size() != int64(len(data)) {
		t.Errorf("Size = %d, want %d", c.Size(), len(data))
	}
	if c.SHA256() != sha(data) {
		t.Errorf("SHA256 = %s, want %s", c.SHA256(), sha(data))
	}
	m := md5.Sum(data)
	if c.MD5() != hex.EncodeToString(m[:]) {
		t.Errorf("MD5 = %s, want %x", c.MD5(), m)
	}
}

func TestReaderChecks(t *testing.T) {
	data := []byte("0123456789")
	tests := []struct {
		name    string
		opts    models.WriteStreamOptions
		readErr error // error expected while reading
		verify  error // error expected from Verify after a clean read
	}{
		{name: "no checks", opts: models.WriteStreamOptions{}},
		{name: "checksum mismatch", opts: models.WriteStreamOptions{ExpectedSHA256: sha([]byte("other"))}, verify: models.ErrChecksumMismatch},
		{name: "stream shorter than expected", opts: models.WriteStreamOptions{ExpectedSize: 11}, verify: models.ErrSizeMismatch},
		{name: "stream longer than expected", opts: models.WriteStreamOptions{ExpectedSize: 9}, readErr: models.ErrSizeMismatch},
		{name: "exactly max size", opts: models.WriteStreamOptions{MaxSize: 10}},
		{name: "over max size", opts: models.WriteStreamOptions{MaxSize: 9}, readErr: models.ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(context.Background(), bytes.NewReader(data), tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, c)
			if !errors.Is(err, tt.readErr) {
				t.Fatalf("read error = %v, want %v", err, tt.readErr)
			}
			if tt.readErr != nil {
				return
			}
			if err := c.Verify(); !errors.Is(err, tt.verify) {
				t.Fatalf("Verify = %v, want %v", err, tt.verify)
			}
		})
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	tests := map[string]models.WriteStreamOptions{
		"sha not hex":            {ExpectedSHA256: strings.Repeat("z", 64)},
		"sha wrong length":       {ExpectedSHA256: "abcd"},
		"negative max size":      {MaxSize: -1},
		"negative expected size": {ExpectedSize: -1},
		"negative chunk size":    {ChunkSize: -1},
		"expected over max":      {ExpectedSize: 11, MaxSize: 10},
	}
	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := New(context.Background(), strings.NewReader("x"), opts); !errors.Is(err, models.ErrInvalidOptions) {
				t.Fatalf("err = %v, want ErrInvalidOptions", err)
			}
		})
	}
	if _, err := New(context.Background(), nil, models.WriteStreamOptions{}); !errors.Is(err, models.ErrInvalidOptions) {
		t.Fatalf("nil reader: err = %v, want ErrInvalidOptions", err)
	}
}

func TestReaderStopsWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, err := New(ctx, strings.NewReader("data"), models.WriteStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.ReadAll(c); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// blockingReader blocks every Read until release is closed.
type blockingReader struct{ release chan struct{} }

func (b *blockingReader) Read(p []byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

func TestReaderCancelInterruptsBlockedRead(t *testing.T) {
	r := &blockingReader{release: make(chan struct{})}
	defer close(r.release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c, err := New(ctx, r, models.WriteStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 10))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read still blocked after the context's deadline")
	}
	if _, err := c.Read(make([]byte, 10)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("later Read err = %v, want the same error", err)
	}
}

func TestVerifyRequiresEOF(t *testing.T) {
	c, err := New(context.Background(), strings.NewReader("0123456789"), models.WriteStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(); err == nil {
		t.Fatal("Verify succeeded before the stream was read to the end")
	}
}

func TestOverflowIsNotCountedOrHashed(t *testing.T) {
	c, err := New(context.Background(), strings.NewReader("0123456789"), models.WriteStreamOptions{MaxSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	n, err := c.Read(make([]byte, 10))
	if n != 0 || !errors.Is(err, models.ErrTooLarge) {
		t.Fatalf("Read = %d, %v; want 0, ErrTooLarge", n, err)
	}
	if c.Size() != 0 || c.SHA256() != sha(nil) {
		t.Errorf("Size = %d, SHA256 = %s; overflowing bytes were counted", c.Size(), c.SHA256())
	}
	if _, err := c.Read(make([]byte, 10)); !errors.Is(err, models.ErrTooLarge) {
		t.Errorf("later Read err = %v, want ErrTooLarge again", err)
	}
}
