//go:build unix

package localFS

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ninjamarcus/ninjaStorage/models"
)

func TestWriteStreamRespectsUmask(t *testing.T) {
	old := syscall.Umask(0077)
	defer syscall.Umask(old)

	fs, dir := newTestFS(t)
	if _, err := fs.Write([]byte("w"), "write.bkup", &models.FileMetaData{}); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.WriteStream(context.Background(), bytes.NewReader([]byte("s")), "stream.bkup", nil, models.WriteStreamOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"write.bkup", "stream.bkup"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("%s permissions = %o under umask 077, want 600", name, perm)
		}
	}
}
