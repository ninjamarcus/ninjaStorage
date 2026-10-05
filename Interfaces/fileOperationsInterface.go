package ninjaStorage

import (
	"context"
	"io"

	"github.com/ninjamarcus/ninjaStorage/models"
)

type FileOperations interface {
	Connect() error
	Write(data []byte, filePath string, metaData *models.FileMetaData) (*models.FileMetaData, error)
	// WriteStream writes r to filePath without holding the whole stream in
	// memory. The file only appears once r has been read to the end and every
	// check in opts has passed; on any error, or if ctx is cancelled, nothing is
	// stored. The returned metadata includes the stream's SHA-256.
	WriteStream(ctx context.Context, r io.Reader, filePath string, metaData *models.FileMetaData, opts models.WriteStreamOptions) (*models.FileMetaData, error)
	Delete(filePath string) error
	Move(filePathFrom string, filePathTo string) error
	Copy(filePathFrom string, filePathTo string) error
	//Something to do with searching the metadata
	Find()
	List(prefix string) (map[string]*models.FileMetaData, error)
	Read(filePath string) ([]byte, *models.FileMetaData, error)
}
