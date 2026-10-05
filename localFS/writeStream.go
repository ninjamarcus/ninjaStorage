package localFS

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ninjamarcus/ninjaStorage/internal/pathcheck"
	"github.com/ninjamarcus/ninjaStorage/internal/streamcheck"
	"github.com/ninjamarcus/ninjaStorage/models"
)

// WriteStream writes r to a temporary file next to the target and renames it
// into place only once r has been read to the end, every check in opts has
// passed and ctx is still live. On failure the temporary file is removed and
// any existing file at the target is left untouched. A successful write
// replaces an existing file.
//
// The file is created with mode 0644 less the process umask, as Write does.
// The data is synced before the rename; syncing the directory afterwards, so
// the rename itself survives a crash, is best effort.
//
// As with Write, user metadata is not stored for local files.
func (fs *LocalFS) WriteStream(ctx context.Context, r io.Reader, name string, _ *models.FileMetaData, opts models.WriteStreamOptions) (_ *models.FileMetaData, err error) {
	if err := pathcheck.Validate(name); err != nil {
		return nil, err
	}
	check, err := streamcheck.New(ctx, r, opts)
	if err != nil {
		return nil, err
	}

	filename := fs.getFilePath(name)
	dir := filepath.Dir(filename)
	if err := ensureParentExists(filename, 0755); err != nil {
		return nil, err
	}
	tmp, err := createTemp(dir, filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = tmp.Close() // already closed on the paths that reach the rename
		if rmErr := os.Remove(tmp.Name()); rmErr != nil && !errors.Is(rmErr, iofs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("removing temporary file %s: %w", tmp.Name(), rmErr))
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
	info, err := tmp.Stat()
	if err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	beforeCommit()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("write of %s aborted: %w", name, err)
	}
	if err := os.Rename(tmp.Name(), filename); err != nil {
		return nil, err
	}
	committed = true
	syncDir(dir)

	return &models.FileMetaData{
		Md5Hash:    check.MD5(),
		Sha256Hash: check.SHA256(),
		Name:       name,
		Size:       info.Size(),
		Updated:    info.ModTime(),
	}, nil
}

// beforeCommit runs just before the final context check; tests replace it to
// cancel at that point.
var beforeCommit = func() {}

// tempSuffix marks WriteStream's in-progress files, which List skips. A
// process that dies mid-write leaves its temporary file behind; nothing removes
// it automatically.
const tempSuffix = ".writestream.tmp"

// maxTempBase keeps temporary names within the usual 255-byte file name limit:
// "." + base + "." + 16 hex characters + tempSuffix.
const maxTempBase = 255 - 1 - 1 - 16 - len(tempSuffix)

func isWriteStreamTemp(base string) bool {
	return strings.HasPrefix(base, ".") && strings.HasSuffix(base, tempSuffix)
}

// createTemp creates an empty temporary file in dir for a write to base. Unlike
// os.CreateTemp, which always uses mode 0600, it asks for 0644 so that the
// umask applies exactly as it does for Write.
func createTemp(dir, base string) (*os.File, error) {
	if len(base) > maxTempBase {
		base = base[:maxTempBase]
	}
	for i := 0; i < 10; i++ {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, "."+base+"."+hex.EncodeToString(suffix[:])+tempSuffix)
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
		if errors.Is(err, iofs.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, fmt.Errorf("could not create a unique temporary file in %s", dir)
}

// syncDir flushes a directory entry change to disk where the platform allows
// it. Errors are ignored: the rename has already happened, and some systems
// cannot sync a directory at all.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
