package ninjaStorage

import (
	"github.com/ninjamarcus/ninjaStorage/models"
)

type FileOperations interface {
	Connect() error
	Write(data []byte, filePath string, metaData *models.FileMetaData) (*models.FileMetaData, error)
	Delete(filePath string) error
	Move(filePathFrom string, filePathTo string) error
	Copy(filePathFrom string, filePathTo string) error
	//Something to do with searching the metadata
	Find()
	List(prefix string) (map[string]*models.FileMetaData, error)
	Read(filePath string) ([]byte, *models.FileMetaData, error)
}
