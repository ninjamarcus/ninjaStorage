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

	ninjaStorage "github.com/ninjamarcus/ninjaStorage/Interfaces"
	"github.com/ninjamarcus/ninjaStorage/models"
)

var _ ninjaStorage.FileOperations = (*GCPFS)(nil)

// fakeGCS implements just enough of the GCS JSON upload API for WriteStream:
// single-request multipart uploads and chunked resumable uploads. An object is
// only recorded in objects once its upload is finalised.
type fakeGCS struct {
	mu       sync.Mutex
	objects  map[string][]byte
	metadata map[string]map[string]string
	sessions map[string]*session
	chunks   int // resumable chunk requests received
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

func (f *fakeGCS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	// Chunks are POSTed to the session URI from the Location header.
	case r.URL.Query().Get("upload_id") != "":
		s, ok := f.sessions[r.URL.Query().Get("upload_id")]
		if !ok {
			http.Error(w, "no such session", http.StatusNotFound)
			return
		}
		f.chunks++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return // client went away mid-chunk
		}
		s.data = append(s.data, body...)
		// "bytes 0-99/*" is an intermediate chunk; "bytes 0-99/100" or
		// "bytes */100" finishes the upload.
		cr := r.Header.Get("Content-Range")
		if strings.HasSuffix(cr, "/*") {
			// The client sends X-GUploader-No-308, so "resume incomplete" is
			// a 200 with this override header rather than a real 308.
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(s.data)-1))
			w.Header().Set("X-Http-Status-Code-Override", "308")
			w.WriteHeader(http.StatusOK)
			return
		}
		f.finalise(w, s.name, s.metadata, s.data)

	case r.Method == http.MethodPost && r.URL.Query().Get("uploadType") == "multipart":
		name, meta, data, err := readMultipart(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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
		id := strconv.Itoa(len(f.sessions) + 1)
		f.sessions[id] = &session{name: obj.Name, metadata: obj.Metadata}
		w.Header().Set("Location", fmt.Sprintf("http://%s%s?uploadType=resumable&upload_id=%s", r.Host, r.URL.Path, id))
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "unexpected request "+r.Method+" "+r.URL.String(), http.StatusNotImplemented)
	}
}

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

func TestWriteStreamUploads(t *testing.T) {
	tests := []struct {
		name      string
		size      int
		chunkSize int
		chunked   bool
	}{
		{name: "smaller than one chunk", size: 1000},
		{name: "several chunks", size: 3*minChunk + 123, chunkSize: minChunk, chunked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, fake := newTestGCPFS(t)
			data := bytes.Repeat([]byte{0xAB, 0xCD, 0xEF}, tt.size/3+1)[:tt.size]
			meta := &models.FileMetaData{UserMetaData: map[string]string{"orgID": "42", "adcID": "7"}}

			md, err := g.WriteStream(context.Background(), bytes.NewReader(data), "42/abc.bkup", meta, models.WriteStreamOptions{
				ExpectedSHA256: hexSHA(data),
				ExpectedSize:   int64(len(data)),
				MaxSize:        int64(len(data)),
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
			if fake.metadata["backups/42/abc.bkup"]["orgID"] != "42" {
				t.Errorf("user metadata not sent with the upload: %v", fake.metadata["backups/42/abc.bkup"])
			}
			if tt.chunked && fake.chunks < 2 {
				t.Errorf("chunk requests = %d, want a multi-chunk upload", fake.chunks)
			}
			m := md5.Sum(data)
			if md.Md5Hash != hex.EncodeToString(m[:]) || md.Sha256Hash != hexSHA(data) || md.Size != int64(len(data)) {
				t.Errorf("metadata = %+v", md)
			}
		})
	}
}

func TestWriteStreamFailureCreatesNoObject(t *testing.T) {
	data := bytes.Repeat([]byte("y"), 3*minChunk+5)
	tests := []struct {
		name string
		opts models.WriteStreamOptions
		want error
	}{
		{name: "checksum mismatch, single request", opts: models.WriteStreamOptions{ExpectedSHA256: hexSHA([]byte("other"))}, want: models.ErrChecksumMismatch},
		{name: "checksum mismatch, chunked", opts: models.WriteStreamOptions{ExpectedSHA256: hexSHA([]byte("other")), ChunkSize: minChunk}, want: models.ErrChecksumMismatch},
		{name: "too large, chunked", opts: models.WriteStreamOptions{MaxSize: 2 * minChunk, ChunkSize: minChunk}, want: models.ErrTooLarge},
		{name: "shorter than expected", opts: models.WriteStreamOptions{ExpectedSize: int64(len(data)) + 1}, want: models.ErrSizeMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, fake := newTestGCPFS(t)
			_, err := g.WriteStream(context.Background(), bytes.NewReader(data), "1/b.bkup", nil, tt.opts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if _, ok := fake.stored("backups/1/b.bkup"); ok {
				t.Fatal("object was created despite the failed check")
			}
		})
	}
}

func TestWriteStreamRejectsBadInput(t *testing.T) {
	g, fake := newTestGCPFS(t)
	if _, err := g.WriteStream(context.Background(), strings.NewReader("x"), "", nil, models.WriteStreamOptions{}); err == nil {
		t.Error("expected an error for an empty path")
	}
	if _, err := g.WriteStream(context.Background(), strings.NewReader("x"), "a", nil, models.WriteStreamOptions{ExpectedSHA256: "nope"}); !errors.Is(err, models.ErrInvalidOptions) {
		t.Errorf("err = %v, want ErrInvalidOptions", err)
	}
	if len(fake.objects) != 0 || len(fake.sessions) != 0 {
		t.Error("bad input reached the server")
	}
}
