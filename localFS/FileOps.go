package localFS

import (
	"io"
	"io/ioutil"
	"os"

	_ "github.com/ninjamarcus/ninjaStorage/Interfaces" /*ninjaStorage*/
	"github.com/ninjamarcus/ninjaStorage/models"
)

type LocalFS struct {
	config *models.LocalFSConfig
}

func NewLocalStorage(config *models.LocalFSConfig) (*LocalFS, error) {
	err := config.Validate()
	if err != nil {
		return &LocalFS{}, err
	}
	return &LocalFS{config: config}, nil
}

func (fs *LocalFS) Connect() error {
	// This is a NO-OP for local storage
	return nil
}

func (fs *LocalFS) Delete(name string) error {
	return os.Remove(fs.getFilePath(name))
}

func (fs *LocalFS) Move(source string, target string) error {
	filename := fs.getFilePath(target)
	err := ensureParentExists(filename, 0755)
	if err != nil {
		return err
	}
	return os.Rename(fs.getFilePath(source), filename)
}

func (fs *LocalFS) Copy(source string, target string) error {
	input, err := os.Open(fs.getFilePath(source))
	if err != nil {
		return err
	}
	defer input.Close()
	filename := fs.getFilePath(target)
	err = ensureParentExists(filename, 0755)
	if err != nil {
		return err
	}
	output, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	if err != nil {
		return err
	}
	return nil
}

func (fs *LocalFS) Find() {
	panic("implement me")
}

func (fs *LocalFS) Write(data []byte, name string, metadata *models.FileMetaData) (*models.FileMetaData, error) {
	filename := fs.getFilePath(name)
	err := ensureParentExists(filename, 0755)
	if err != nil {
		return nil, err
	}
	err = ioutil.WriteFile(filename, data, 0644)
	if err != nil {
		return nil, err
	}
	result, err := getMetaData(name, filename)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (fs *LocalFS) List(prefix string) (map[string]*models.FileMetaData, error) {
	return search(fs.config.FS.ParentFolder, prefix)
}

func (fs *LocalFS) Read(name string) ([]byte, *models.FileMetaData, error) {
	filename := fs.getFilePath(name)
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, nil, err
	}
	metadata, err := getMetaData(name, filename)
	if err != nil {
		return nil, nil, err
	}
	return data, metadata, nil
}
