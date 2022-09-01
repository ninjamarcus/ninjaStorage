package localFS

import (
	ninjaStorage "github.com/ninjamarcus/ninjaStorage/Interfaces"
	"github.com/ninjamarcus/ninjaStorage/models"
)

type LocalFS struct {
	ninjaStorage.FileOperations
}

func NewLocalStorage(fs *models.LOCALFSConfig) (*LocalFS, error) {
	return &LocalFS{}, nil
}
