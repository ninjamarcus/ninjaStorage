package localFS

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ninjamarcus/ninjaStorage/internal/streamcheck"
	"github.com/ninjamarcus/ninjaStorage/models"
)

// WriteStream writes r to a temporary file next to the target and renames it
// into place only once r has been read to the end and every check in opts has
// passed. On failure the temporary file is removed and any existing file at
// the target is left untouched.
//
// As with Write, user metadata is not stored for local files.
func (fs *LocalFS) WriteStream(ctx context.Context, r io.Reader, name string, _ *models.FileMetaData, opts models.WriteStreamOptions) (*models.FileMetaData, error) {
	if name == "" {
		return nil, fmt.Errorf("Filepath cannot be empty")
	}
	check, err := streamcheck.New(ctx, r, opts)
	if err != nil {
		return nil, err
	}

	filename := fs.getFilePath(name)
	if err := ensureParentExists(filename, 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), "."+filepath.Base(filename)+".*"+tempSuffix)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err := io.Copy(tmp, check); err != nil {
		return nil, fmt.Errorf("write of %s aborted: %w", name, err)
	}
	if err := check.Verify(); err != nil {
		return nil, fmt.Errorf("write of %s aborted: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}
	if err := tmp.Chmod(0644); err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), filename); err != nil {
		return nil, err
	}
	committed = true

	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	return &models.FileMetaData{
		Md5Hash:    check.MD5(),
		Sha256Hash: check.SHA256(),
		Name:       name,
		Size:       info.Size(),
		Updated:    info.ModTime(),
	}, nil
}

// tempSuffix marks WriteStream's in-progress files, which List skips.
const tempSuffix = ".writestream.tmp"

func isWriteStreamTemp(base string) bool {
	return strings.HasPrefix(base, ".") && strings.HasSuffix(base, tempSuffix)
}
