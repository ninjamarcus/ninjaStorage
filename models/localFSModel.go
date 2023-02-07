package models

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// TODO: extra configuration details for the local fs stuff go in here
type LocalFSConfig struct {
	*FS
}

func (config *LocalFSConfig) Validate() error {
	directory := config.FS.ParentFolder
	if directory == "" {
		return errors.New("ParentFolder has not been set")
	}
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("ParentFolder [%s] is not an absolute path", directory)
	}
	file, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("Error validating ParentFolder: %s", err.Error())
	}
	if !file.IsDir() {
		return fmt.Errorf("ParentFolder [%s] is not a directory", directory)
	}
	return nil
}
