package localFS

import (
	storage "storage/Interfaces"
	"storage/models"
)

type LocalFS struct {
	storage.FileOperations
}

func NewLocalStorage(fs *models.LOCALFSConfig) (*LocalFS, error) {
	return &LocalFS{}, nil
}
