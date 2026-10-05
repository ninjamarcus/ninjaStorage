package gcpFS

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ninjaStorage "github.com/ninjamarcus/ninjaStorage/Interfaces"
	"github.com/ninjamarcus/ninjaStorage/models"
)

var _ ninjaStorage.FileOperations = (*GCPFS)(nil)

// fakeGCS implements just enough of the GCS JSON upload API for WriteStream:
// single-request multipart uploads and chunked resumable uploads. An object is
// only recorded in objects once its upload is finalised. Chunks must arrive at
// the right offset, and the final size must match, or the request fails.
type fakeGCS struct {
	mu       sync.Mutex
	objects  map[string][]byte
	metadata map[string]map[string]string
	sessions map[string]*session
	chunks   int // resumable chunk requests received

	// chunkStatus, if set, is called for each chunk request before it is
	// applied. A non-zero status is returned instead, and the chunk dropped.
	chunkStatus func(n int, final bool) int
	// blockChunks makes chunk requests wait until the client gives up. Each one
	// signals on blocked first.
	blockChunks bool
	blocked     chan struct{}
}

type session struct {
	name     string
	metadata map[string]string
	data     []byte
}

func newFakeGCS(t *testing.T) *fakeGCS {
	f := &fakeGCS{
		objects:  map[string][]byte{},
		metadata: map[string]map[string]string{},
		sessions: map[string]*session{},
		blocked:  make(chan struct{}, 1),
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(srv.URL, "http://"))
	return f
}

func (f *fakeGCS) stored(name string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[name]
	return b, ok
}

func (f *fakeGCS) put(name string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[name] = data
}

func (f *fakeGCS) counts() (sessions, chunks int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions), f.chunks
}

// parseContentRange parses "bytes a-b/*" (an intermediate chunk), "bytes
// a-b/N" (the final chunk) and "bytes */N" (an empty final request). start and
// total are -1 when not given.
func parseContentRange(h string) (start, total int, err error) {
	spec, ok := strings.CutPrefix(h, "bytes ")
	rng, size, ok2 := strings.Cut(spec, "/")
	if !ok || !ok2 {
		return 0, 0, fmt.Errorf("bad Content-Range %q", h)
	}
	start, total = -1, -1
	if rng != "*" {
		first, _, _ := strings.Cut(rng, "-")
		if start, err = strconv.Atoi(first); err != nil {
			return 0, 0, fmt.Errorf("bad Content-Range %q", h)
		}
	}
	if size != "*" {
		if total, err = strconv.Atoi(size); err != nil {
			return 0, 0, fmt.Errorf("bad Content-Range %q", h)
		}
	}
	return start, total, nil
}

func (f *fakeGCS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	// Chunks are POSTed to the session URI from the Location header.
	case r.URL.Query().Get("upload_id") != "":
		f.serveChunk(w, r)

	case r.Method == http.MethodPost && r.URL.Query().Get("uploadType") == "multipart":
		name, meta, data, err := readMultipart(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.finalise(w, name, meta, data)

	case r.Method == http.MethodPost && r.URL.Query().Get("uploadType") == "resumable":
		var obj struct {
			Name     string            `json:"name"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		id := strconv.Itoa(len(f.sessions) + 1)
		f.sessions[id] = &session{name: obj.Name, metadata: obj.Metadata}
		w.Header().Set("Location", fmt.Sprintf("http://%s%s?uploadType=resumable&upload_id=%s", r.Host, r.URL.Path, id))
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "unexpected request "+r.Method+" "+r.URL.String(), http.StatusNotImplemented)
	}
}

func (f *fakeGCS) serveChunk(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return // client went away mid-chunk
	}
	start, total, err := parseContentRange(r.Header.Get("Content-Range"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	final := total >= 0

	f.mu.Lock()
	s, ok := f.sessions[r.URL.Query().Get("upload_id")]
	f.chunks++
	n, hook, block := f.chunks, f.chunkStatus, f.blockChunks
	f.mu.Unlock()
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if block {
		select {
		case f.blocked <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		return
	}
	if hook != nil {
		if status := hook(n, final); status != 0 {
			http.Error(w, "injected failure", status)
			return
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if start >= 0 && start != len(s.data) {
		http.Error(w, fmt.Sprintf("chunk starts at %d, have %d bytes", start, len(s.data)), http.StatusBadRequest)
		return
	}
	s.data = append(s.data, body...)
	if !final {
		// The client sends X-GUploader-No-308, so "resume incomplete" is a 200
		// with this override header rather than a real 308.
		if len(s.data) > 0 {
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(s.data)-1))
		}
		w.Header().Set("X-Http-Status-Code-Override", "308")
		w.WriteHeader(http.StatusOK)
		return
	}
	if total != len(s.data) {
		http.Error(w, fmt.Sprintf("final size %d, have %d bytes", total, len(s.data)), http.StatusBadRequest)
		return
	}
	f.finalise(w, s.name, s.metadata, s.data)
}

// finalise stores an object and writes its JSON resource. Call with f.mu held.
func (f *fakeGCS) finalise(w http.ResponseWriter, name string, meta map[string]string, data []byte) {
	f.objects[name] = data
	f.metadata[name] = meta
	m := md5.Sum(data)
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"bucket":   "test-bucket",
		"name":     name,
		"size":     strconv.Itoa(len(data)),
		"md5Hash":  base64.StdEncoding.EncodeToString(m[:]),
		"crc32c":   base64.StdEncoding.EncodeToString(crc),
		"metadata": meta,
	})
}

func readMultipart(r *http.Request) (string, map[string]string, []byte, error) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return "", nil, nil, err
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	part, err := mr.NextPart()
	if err != nil {
		return "", nil, nil, err
	}
	var obj struct {
		Name     string            `json:"name"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(part).Decode(&obj); err != nil {
		return "", nil, nil, err
	}
	part, err = mr.NextPart()
	if err != nil {
		return "", nil, nil, err
	}
	data, err := io.ReadAll(part)
	return obj.Name, obj.Metadata, data, err
}

func newTestGCPFS(t *testing.T) (*GCPFS, *fakeGCS) {
	t.Helper()
	fake := newFakeGCS(t)
	g, err := NewGCPStorage(&models.GCPFSConfig{BucketName: "test-bucket", FS: &models.FS{ParentFolder: "backups"}})
	if err != nil {
		t.Fatal(err)
	}
	return g, fake
}

func hexSHA(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Smallest chunk size GCS allows; used to force multi-chunk resumable uploads.
const minChunk = 256 * 1024

func pattern(size int) []byte {
	return bytes.Repeat([]byte{0xAB, 0xCD, 0xEF}, size/3+1)[:size]
}

func TestWriteStreamUploads(t *testing.T) {
	tests := []struct {
		name       string
		size       int
		chunkSize  int
		minChunks  int // chunk requests expected, 0 for a single-request upload
		wantResume bool
	}{
		{name: "smaller than one chunk", size: 1000},
		{name: "empty stream", size: 0},
		{name: "several chunks", size: 3*minChunk + 123, chunkSize: minChunk, minChunks: 2, wantResume: true},
		{name: "exact multiple of the chunk size", size: 3 * minChunk, chunkSize: minChunk, minChunks: 3, wantResume: true},
		// With ChunkSize left at zero, WriteStream must still chunk the upload:
		// that is what makes aborting safe.
		{name: "default chunk size is used", size: models.DefaultChunkSize + 10, minChunks: 2, wantResume: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, fake := newTestGCPFS(t)
			data := pattern(tt.size)
			meta := &models.FileMetaData{UserMetaData: map[string]string{"orgID": "42", "adcID": "7"}}

			md, err := g.WriteStream(context.Background(), bytes.NewReader(data), "42/abc.bkup", meta, models.WriteStreamOptions{
				ExpectedSHA256: hexSHA(data),
				MaxSize:        int64(len(data)) + 1,
				ChunkSize:      tt.chunkSize,
			})
			if err != nil {
				t.Fatalf("WriteStream: %v", err)
			}

			got, ok := fake.stored("backups/42/abc.bkup")
			if !ok {
				t.Fatal("object was not created")
			}
			if !bytes.Equal(got, data) {
				t.Fatal("stored object differs from the stream")
			}
			fake.mu.Lock()
			orgID := fake.metadata["backups/42/abc.bkup"]["orgID"]
			fake.mu.Unlock()
			if orgID != "42" {
				t.Error("user metadata not sent with the upload")
			}
			sessions, chunks := fake.counts()
			if tt.wantResume && (sessions != 1 || chunks < tt.minChunks) {
				t.Errorf("sessions = %d, chunk requests = %d; want a chunked upload of at least %d chunks", sessions, chunks, tt.minChunks)
			}
			m := md5.Sum(data)
			if md.Name != "backups/42/abc.bkup" || md.Md5Hash != hex.EncodeToString(m[:]) || md.Sha256Hash != hexSHA(data) || md.Size != int64(len(data)) {
				t.Errorf("metadata = %+v", md)
			}
		})
	}
}

// failingReader returns data, then fails.
type failingReader struct {
	r     io.Reader
	after int
}

var errConnectionReset = errors.New("connection reset")

func (f *failingReader) Read(p []byte) (int, error) {
	if f.after <= 0 {
		return 0, errConnectionReset
	}
	if len(p) > f.after {
		p = p[:f.after]
	}
	n, err := f.r.Read(p)
	f.after -= n
	return n, err
}

// cancellingReader cancels the write once it has returned after bytes.
type cancellingReader struct {
	r      io.Reader
	after  int
	cancel context.CancelFunc
}

func (c *cancellingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if c.after -= n; c.after <= 0 {
		c.cancel()
	}
	return n, err
}

// TestWriteStreamFailureKeepsExistingObject checks that a failed write neither
// creates an object nor replaces one that is already there.
func TestWriteStreamFailureKeepsExistingObject(t *testing.T) {
	data := pattern(3*minChunk + 5)
	exact := pattern(3 * minChunk)
	other := hexSHA([]byte("other"))
	tests := []struct {
		name string
		data []byte
		// reader wraps the data; nil uses it as-is.
		reader func(r io.Reader, cancel context.CancelFunc) io.Reader
		opts   models.WriteStreamOptions
		cancel bool // cancel before calling WriteStream
		want   error
	}{
		{name: "checksum mismatch, single request", data: data, opts: models.WriteStreamOptions{ExpectedSHA256: other}, want: models.ErrChecksumMismatch},
		{name: "checksum mismatch, chunked", data: data, opts: models.WriteStreamOptions{ExpectedSHA256: other, ChunkSize: minChunk}, want: models.ErrChecksumMismatch},
		{name: "checksum mismatch, exact multiple of the chunk size", data: exact, opts: models.WriteStreamOptions{ExpectedSHA256: other, ChunkSize: minChunk}, want: models.ErrChecksumMismatch},
		{name: "checksum mismatch, default chunk size", data: pattern(models.DefaultChunkSize + 10), opts: models.WriteStreamOptions{ExpectedSHA256: other}, want: models.ErrChecksumMismatch},
		{name: "too large, chunked", data: data, opts: models.WriteStreamOptions{MaxSize: 2 * minChunk, ChunkSize: minChunk}, want: models.ErrTooLarge},
		{name: "shorter than expected", data: data, opts: models.WriteStreamOptions{ExpectedSize: int64(len(data)) + 1}, want: models.ErrSizeMismatch},
		{name: "reader fails mid-stream, single request", data: data, want: errConnectionReset,
			reader: func(r io.Reader, _ context.CancelFunc) io.Reader { return &failingReader{r: r, after: 2*minChunk + 7} }},
		{name: "reader fails mid-stream, chunked", data: data, opts: models.WriteStreamOptions{ChunkSize: minChunk}, want: errConnectionReset,
			reader: func(r io.Reader, _ context.CancelFunc) io.Reader { return &failingReader{r: r, after: 2*minChunk + 7} }},
		{name: "reader fails before any data", data: data, want: errConnectionReset,
			reader: func(r io.Reader, _ context.CancelFunc) io.Reader { return &failingReader{r: r} }},
		{name: "cancelled mid-stream, single request", data: data, want: context.Canceled,
			reader: func(r io.Reader, cancel context.CancelFunc) io.Reader {
				return &cancellingReader{r: r, after: 2*minChunk + 7, cancel: cancel}
			}},
		{name: "cancelled mid-stream, chunked", data: data, opts: models.WriteStreamOptions{ChunkSize: minChunk}, want: context.Canceled,
			reader: func(r io.Reader, cancel context.CancelFunc) io.Reader {
				return &cancellingReader{r: r, after: 2*minChunk + 7, cancel: cancel}
			}},
		{name: "cancelled as the stream ends", data: data, opts: models.WriteStreamOptions{ChunkSize: minChunk}, want: context.Canceled,
			reader: func(r io.Reader, cancel context.CancelFunc) io.Reader {
				return &cancellingReader{r: r, after: len(data), cancel: cancel}
			}},
		{name: "cancelled before the call", data: data, cancel: true, want: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, fake := newTestGCPFS(t)
			fake.put("backups/1/b.bkup", []byte("original"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			var r io.Reader = bytes.NewReader(tt.data)
			if tt.reader != nil {
				r = tt.reader(r, cancel)
			}

			md, err := g.WriteStream(ctx, r, "1/b.bkup", nil, tt.opts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if md != nil {
				t.Errorf("metadata = %+v, want nil", md)
			}
			if got, _ := fake.stored("backups/1/b.bkup"); string(got) != "original" {
				t.Fatalf("existing object replaced by %d bytes", len(got))
			}
		})
	}
}

func TestWriteStreamCommitFailure(t *testing.T) {
	g, fake := newTestGCPFS(t)
	fake.chunkStatus = func(_ int, final bool) int {
		if final {
			return http.StatusForbidden
		}
		return 0
	}
	data := pattern(3*minChunk + 5)
	md, err := g.WriteStream(context.Background(), bytes.NewReader(data), "1/b.bkup", nil,
		models.WriteStreamOptions{ExpectedSHA256: hexSHA(data), ChunkSize: minChunk})
	if err == nil || md != nil {
		t.Fatalf("WriteStream = %+v, %v; want nil metadata and an error", md, err)
	}
	if _, ok := fake.stored("backups/1/b.bkup"); ok {
		t.Fatal("object was created despite the failed commit")
	}
}

func TestWriteStreamRetriesFailedChunk(t *testing.T) {
	g, fake := newTestGCPFS(t)
	failed := false
	fake.chunkStatus = func(n int, _ bool) int {
		if n == 2 && !failed {
			failed = true
			return http.StatusServiceUnavailable
		}
		return 0
	}
	data := pattern(3*minChunk + 5)
	if _, err := g.WriteStream(context.Background(), bytes.NewReader(data), "1/b.bkup", nil,
		models.WriteStreamOptions{ExpectedSHA256: hexSHA(data), ChunkSize: minChunk}); err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	if !failed {
		t.Fatal("the injected failure never happened")
	}
	if got, _ := fake.stored("backups/1/b.bkup"); !bytes.Equal(got, data) {
		t.Fatal("stored object differs from the stream after a retried chunk")
	}
}

func TestWriteStreamCancelWhileServerStalls(t *testing.T) {
	g, fake := newTestGCPFS(t)
	fake.blockChunks = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-fake.blocked
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := g.WriteStream(ctx, bytes.NewReader(pattern(3*minChunk)), "1/b.bkup", nil,
			models.WriteStreamOptions{ChunkSize: minChunk})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WriteStream still blocked after cancel")
	}
	if _, ok := fake.stored("backups/1/b.bkup"); ok {
		t.Fatal("object was created")
	}
}

// stalledReader never returns until released.
type stalledReader struct{ release chan struct{} }

func (s *stalledReader) Read(p []byte) (int, error) {
	<-s.release
	return 0, io.EOF
}

func TestWriteStreamStalledReaderStopsAtDeadline(t *testing.T) {
	g, fake := newTestGCPFS(t)
	r := &stalledReader{release: make(chan struct{})}
	defer close(r.release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := g.WriteStream(ctx, r, "1/b.bkup", nil, models.WriteStreamOptions{})
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
	if _, ok := fake.stored("backups/1/b.bkup"); ok {
		t.Fatal("object was created")
	}
}

func TestWriteStreamRejectsBadInput(t *testing.T) {
	g, fake := newTestGCPFS(t)
	for _, name := range []string{"", "../other-tenant/x", "/abs", "a/../../x"} {
		if _, err := g.WriteStream(context.Background(), strings.NewReader("x"), name, nil, models.WriteStreamOptions{}); !errors.Is(err, models.ErrInvalidPath) {
			t.Errorf("WriteStream(%q) err = %v, want ErrInvalidPath", name, err)
		}
	}
	if _, err := g.WriteStream(context.Background(), strings.NewReader("x"), "a", nil, models.WriteStreamOptions{ExpectedSHA256: "nope"}); !errors.Is(err, models.ErrInvalidOptions) {
		t.Errorf("err = %v, want ErrInvalidOptions", err)
	}
	if sessions, chunks := fake.counts(); sessions != 0 || chunks != 0 || len(fake.objects) != 0 {
		t.Error("bad input reached the server")
	}
}
