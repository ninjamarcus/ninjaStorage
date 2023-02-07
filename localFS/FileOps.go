package localFS

import (
	_ "github.com/ninjamarcus/ninjaStorage/Interfaces" /*ninjaStorage*/
	"github.com/ninjamarcus/ninjaStorage/models"
)

type LocalFS struct {
	config *models.LocalFSConfig
}

func NewLocalStorage(fs *models.LocalFSConfig) (*LocalFS, error) {
	return &LocalFS{}, nil
}

func (fs *LocalFS) Connect() error {
	return nil
}

func (fs *LocalFS) Delete(filePath string) error {
	return nil
}

func (fs *LocalFS) Move(filePathFrom string, filePathTo string) error {
	return nil
}

func (fs *LocalFS) Copy(filePathFrom string, filePathTo string) error {
	return nil
}

func (fs *LocalFS) Find() {
	panic("implement me")
}

func (fs *LocalFS) Write(data []byte, filePath string, metaData *models.FileMetaData) (*models.FileMetaData, error) {

	return &models.FileMetaData{}, nil
}

func (fs *LocalFS) List(prefix string) (map[string]*models.FileMetaData, error) {
	results := make(map[string]*models.FileMetaData)
	return results, nil
}

func (fs *LocalFS) Read(filePath string) ([]byte, *models.FileMetaData, error) {
	return []byte{}, &models.FileMetaData{}, nil
}
