package localFS

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ninjaStorage "github.com/ninjamarcus/ninjaStorage/Interfaces"
	"github.com/ninjamarcus/ninjaStorage/models"
)

var _ ninjaStorage.FileOperations = (*LocalFS)(nil)

func newTestFS(t *testing.T) (*LocalFS, string) {
	t.Helper()
	dir := t.TempDir()
	fs, err := NewLocalStorage(&models.LocalFSConfig{FS: &models.FS{ParentFolder: dir}})
	if err != nil {
		t.Fatal(err)
	}
	return fs, dir
}

func hexSHA(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func hexMD5(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

// entries lists every file under dir, so tests can spot leftover temp files.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWriteStreamStoresFile(t *testing.T) {
	fs, dir := newTestFS(t)
	data := bytes.Repeat([]byte("backup data "), 100000)

	md, err := fs.WriteStream(context.Background(), bytes.NewReader(data), "42/abc.bkup", nil, models.WriteStreamOptions{
		ExpectedSHA256: hexSHA(data),
		ExpectedSize:   int64(len(data)),
		MaxSize:        int64(len(data)),
	})
	if err != nil {
		t.Fatalf("WriteStream: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "42", "abc.bkup"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("stored data differs from the stream")
	}
	if md.Name != "42/abc.bkup" || md.Size != int64(len(data)) {
		t.Errorf("metadata name/size = %q/%d", md.Name, md.Size)
	}
	if md.Sha256Hash != hexSHA(data) {
		t.Errorf("Sha256Hash = %s, want %s", md.Sha256Hash, hexSHA(data))
	}
	if md.Md5Hash != hexMD5(data) {
		t.Errorf("Md5Hash = %s, want %s", md.Md5Hash, hexMD5(data))
	}
	info, err := os.Stat(filepath.Join(dir, "42", "abc.bkup"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Errorf("permissions = %o, want 644", perm)
	}
	if files := entries(t, dir); len(files) != 1 {
		t.Errorf("files on disk = %v, want only the backup", files)
	}
}

type failingReader struct{ after int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.after <= 0 {
		return 0, errors.New("connection reset")
	}
	n := len(p)
	if n > f.after {
		n = f.after
	}
	f.after -= n
	return n, nil
}

func TestWriteStreamFailureStoresNothing(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1<<20)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		r    io.Reader
		opts models.WriteStreamOptions
		want error
	}{
		{name: "checksum mismatch", r: bytes.NewReader(data), opts: models.WriteStreamOptions{ExpectedSHA256: hexSHA([]byte("other"))}, want: models.ErrChecksumMismatch},
		{name: "too large", r: bytes.NewReader(data), opts: models.WriteStreamOptions{MaxSize: 1000}, want: models.ErrTooLarge},
		{name: "truncated", r: bytes.NewReader(data[:100]), opts: models.WriteStreamOptions{ExpectedSize: int64(len(data))}, want: models.ErrSizeMismatch},
		{name: "reader error mid-stream", r: &failingReader{after: 5000}},
		{name: "context cancelled", ctx: cancelled, r: bytes.NewReader(data), want: context.Canceled},
		{name: "invalid options", r: bytes.NewReader(data), opts: models.WriteStreamOptions{ExpectedSHA256: "nope"}, want: models.ErrInvalidOptions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, dir := newTestFS(t)
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			md, err := fs.WriteStream(ctx, tt.r, "1/b.bkup", nil, tt.opts)
			if err == nil {
				t.Fatalf("WriteStream succeeded with %+v, want an error", md)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if files := entries(t, dir); len(files) != 0 {
				t.Errorf("files left on disk: %v", files)
			}
		})
	}
}

func TestWriteStreamFailureKeepsExistingFile(t *testing.T) {
	fs, dir := newTestFS(t)
	original := []byte("original backup")
	if _, err := fs.Write(original, "b.bkup", &models.FileMetaData{}); err != nil {
		t.Fatal(err)
	}

	_, err := fs.WriteStream(context.Background(), bytes.NewReader([]byte("replacement")), "b.bkup", nil,
		models.WriteStreamOptions{ExpectedSHA256: hexSHA([]byte("something else"))})
	if !errors.Is(err, models.ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "b.bkup"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("existing file changed to %q", got)
	}
	if files := entries(t, dir); len(files) != 1 {
		t.Errorf("files on disk = %v, want only the original", files)
	}
}

func TestWriteStreamRejectsEmptyPath(t *testing.T) {
	fs, _ := newTestFS(t)
	if _, err := fs.WriteStream(context.Background(), bytes.NewReader([]byte("x")), "", nil, models.WriteStreamOptions{}); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

func TestWriteReturnsMD5(t *testing.T) {
	fs, _ := newTestFS(t)
	data := []byte("backup")
	md, err := fs.Write(data, "a.bkup", &models.FileMetaData{})
	if err != nil {
		t.Fatal(err)
	}
	if md.Md5Hash != hexMD5(data) {
		t.Errorf("Md5Hash = %q, want %s", md.Md5Hash, hexMD5(data))
	}
}

func TestListSkipsInProgressWriteStream(t *testing.T) {
	fs, dir := newTestFS(t)
	if _, err := fs.Write([]byte("done"), "1/a.bkup", &models.FileMetaData{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1", ".b.bkup.123"+tempSuffix), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := fs.List("1/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["1/a.bkup"] == nil {
		t.Errorf("List = %v, want only 1/a.bkup", got)
	}
}

func TestWriteStreamCancelBeforeCommitStoresNothing(t *testing.T) {
	fs, dir := newTestFS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	beforeCommit = cancel
	defer func() { beforeCommit = func() {} }()

	if _, err := fs.WriteStream(ctx, bytes.NewReader([]byte("complete stream")), "a.bkup", nil, models.WriteStreamOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if files := entries(t, dir); len(files) != 0 {
		t.Errorf("files left on disk: %v", files)
	}
}

// stalledReader never returns until released.
type stalledReader struct{ release chan struct{} }

func (s *stalledReader) Read(p []byte) (int, error) {
	<-s.release
	return 0, io.EOF
}

func TestWriteStreamStalledReaderStopsAtDeadline(t *testing.T) {
	fs, dir := newTestFS(t)
	r := &stalledReader{release: make(chan struct{})}
	defer close(r.release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := fs.WriteStream(ctx, r, "a.bkup", nil, models.WriteStreamOptions{})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WriteStream still blocked after the context's deadline")
	}
	if files := entries(t, dir); len(files) != 0 {
		t.Errorf("files left on disk: %v", files)
	}
}

func TestWriteStreamRejectsUnsafePaths(t *testing.T) {
	fs, dir := newTestFS(t)
	for _, name := range []string{"", "../escaped", "a/../../escaped", "/abs", `a\b`, "."} {
		_, err := fs.WriteStream(context.Background(), bytes.NewReader([]byte("x")), name, nil, models.WriteStreamOptions{})
		if !errors.Is(err, models.ErrInvalidPath) {
			t.Errorf("WriteStream(%q) err = %v, want ErrInvalidPath", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped")); !os.IsNotExist(err) {
		t.Error("a file was written outside ParentFolder")
	}
	if files := entries(t, dir); len(files) != 0 {
		t.Errorf("files left on disk: %v", files)
	}
}

func TestWriteStreamLongName(t *testing.T) {
	fs, dir := newTestFS(t)
	name := strings.Repeat("n", 240) + ".bkup"
	if _, err := fs.WriteStream(context.Background(), bytes.NewReader([]byte("x")), name, nil, models.WriteStreamOptions{}); err != nil {
		t.Fatalf("WriteStream with a %d-byte name: %v", len(name), err)
	}
	if files := entries(t, dir); len(files) != 1 || files[0] != name {
		t.Errorf("files on disk = %v", files)
	}
}

func TestWriteStreamEmptyStream(t *testing.T) {
	fs, dir := newTestFS(t)
	md, err := fs.WriteStream(context.Background(), bytes.NewReader(nil), "empty", nil,
		models.WriteStreamOptions{ExpectedSHA256: hexSHA(nil)})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "empty"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 || md.Size != 0 || md.Sha256Hash != hexSHA(nil) || md.Md5Hash != hexMD5(nil) {
		t.Errorf("size %d, metadata %+v", info.Size(), md)
	}
}

func TestWriteStreamConcurrentWritesToSamePath(t *testing.T) {
	fs, dir := newTestFS(t)
	const writers = 16
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('a' + i)}, 64*1024)
	}

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(p []byte) {
			defer wg.Done()
			if _, err := fs.WriteStream(context.Background(), bytes.NewReader(p), "same.bkup", nil,
				models.WriteStreamOptions{ExpectedSHA256: hexSHA(p)}); err != nil {
				t.Error(err)
			}
		}(payloads[i])
	}
	wg.Wait()

	got, err := os.ReadFile(filepath.Join(dir, "same.bkup"))
	if err != nil {
		t.Fatal(err)
	}
	complete := false
	for _, p := range payloads {
		if bytes.Equal(got, p) {
			complete = true
		}
	}
	if !complete {
		t.Error("final file is not one complete payload")
	}
	if files := entries(t, dir); len(files) != 1 {
		t.Errorf("files on disk = %v, want only the target", files)
	}
}
