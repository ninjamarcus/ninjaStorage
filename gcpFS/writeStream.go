package gcpFS

import (
	"context"
	"fmt"
	"io"
	"path"

	"github.com/ninjamarcus/ninjaStorage/internal/streamcheck"
	"github.com/ninjamarcus/ninjaStorage/models"
)

// WriteStream uploads r to the bucket in ChunkSize chunks, so memory use is
// bounded by one chunk however large the stream is, and each chunk is retried
// on a transient error. The object is only created once r has been read to the
// end and every check in opts has passed: on failure the context passed to the
// writer is cancelled before Close, which discards the upload.
//
// The deadline comes from ctx; there is no fixed timeout as there is for Write.
// User metadata is set on the object as part of the upload.
func (g *GCPFS) WriteStream(ctx context.Context, r io.Reader, filePath string, metaData *models.FileMetaData, opts models.WriteStreamOptions) (*models.FileMetaData, error) {
	if filePath == "" {
		return nil, fmt.Errorf("Filepath cannot be empty")
	}
	check, err := streamcheck.New(ctx, r, opts)
	if err != nil {
		return nil, err
	}

	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	fullPath := path.Join(g.config.ParentFolder, filePath)
	w := g.client.Bucket(g.config.BucketName).Object(fullPath).NewWriter(uploadCtx)
	w.ChunkSize = opts.ChunkSize
	if w.ChunkSize == 0 {
		w.ChunkSize = models.DefaultChunkSize
	}
	if metaData != nil && len(metaData.UserMetaData) > 0 {
		w.Metadata = metaData.UserMetaData
	}

	abort := func(cause error) (*models.FileMetaData, error) {
		cancel()
		_ = w.Close()
		return nil, cause
	}
	if _, err := io.Copy(w, check); err != nil {
		return abort(fmt.Errorf("upload of %s aborted: %w", fullPath, err))
	}
	if err := check.Verify(); err != nil {
		return abort(fmt.Errorf("upload of %s aborted: %w", fullPath, err))
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("Writer.Close error: %w", err)
	}

	result := g.parseMetaData(w.Attrs())
	result.Sha256Hash = check.SHA256()
	return result, nil
}
