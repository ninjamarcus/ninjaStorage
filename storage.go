package ninjaStorage

import (
	"github.com/ninjamarcus/ninjaStorage/gcpFS"
	"github.com/ninjamarcus/ninjaStorage/localFS"
	"github.com/ninjamarcus/ninjaStorage/models"
)

// NewStorageObj baseFolder is the folder with which all further writes will exist
func NewStorageGCP(config *models.GCPFSConfig) (*gcpFS.GCPFS, error) {
	return gcpFS.NewGCPStorage(config)
}

func NewStorageLocal(config *models.LocalFSConfig) (*localFS.LocalFS, error) {
	return localFS.NewLocalStorage(config)
}
