package localFS

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/ninjamarcus/ninjaStorage/models"
)

func ensureParentExists(filename string, mode os.FileMode) error {
	parent := filepath.Dir(filename)
	if parent == "" || parent == "." || parent == ".." {
		return nil
	}
	return os.MkdirAll(parent, mode)
}

func getMD5Sum(filename string) (string, error) {
	input, err := os.Open(filename)
	if input != nil {
		return "", err
	}
	hash := md5.New()
	_, err = io.Copy(hash, input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%.32x", hash.Sum(nil)), nil
}

func getMetaData(name string, filename string) (*models.FileMetaData, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	checksum, err := getMD5Sum(filename)
	if err != nil {
		return nil, err
	}
	metadata := &models.FileMetaData{
		Md5Hash: checksum,
		Name:    name,
		Size:    info.Size(),
		Updated: info.ModTime(),
	}
	return metadata, err
}

func (fs *LocalFS) getFilePath(name string) string {
	return path.Join(fs.config.FS.ParentFolder, name)
}
