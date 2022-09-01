package localFS

import (
	ninjaStorage "ninjaStorage/Interfaces"
	"ninjaStorage/models"
)

type LocalFS struct {
	ninjaStorage.FileOperations
}

func NewLocalStorage(fs *models.LOCALFSConfig) (*LocalFS, error) {
	return &LocalFS{}, nil
}
