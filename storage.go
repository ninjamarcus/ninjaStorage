package ninjaStorage

import (
	"storage/gcpFS"
	"storage/localFS"
	"storage/models"
)

// NewStorageObj baseFolder is the folder with which all further writes will exist
func NewStorageGCP(fs *models.GCPFSConfig) (*gcpFS.GCPFS, error) {
	return gcpFS.NewGCPStorage(fs)
}

func NewStorageLocal(fs *models.LOCALFSConfig) (*localFS.LocalFS, error) {
	return localFS.NewLocalStorage(fs)
}
