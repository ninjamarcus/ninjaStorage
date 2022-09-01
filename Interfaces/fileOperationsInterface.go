package ninjaStorage

import "ninjaStorage/models"

type FileOperations interface {
	Write(data []byte, filePath string, metaData *models.FileMetaData) error
	Delete(filePath string) error
	Move(filePathFrom string, filePathTo string) error
	Copy(filePathFrom string, filePathTo string) error
	//Something to do with searching the metadata
	Find()
	List(prefix string) (map[string]*models.FileMetaData, error)
	Read(filePath string) ([]byte, error)
}
