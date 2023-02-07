package models

// TODO: extra configuration details for the local fs stuff go in here
type LocalFSConfig struct {
	*FS
}

func (config *LocalFSConfig) Validate() error {
	return nil
}
