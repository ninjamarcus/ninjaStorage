package models

import "errors"

// For Authentication you need to set your environment variable GOOGLE_APPLICATION_CREDENTIALS
type GCPFSConfig struct {
	BucketName string
	//I might not need project ID
	ProjectID string
	*FS
}

func (config *GCPFSConfig) Validate() error {
	if config.ParentFolder == "" {
		return errors.New("ParentFolder has not been set")
	}
	if config.BucketName == "" {
		return errors.New("BucketName has not been set")
	}
	// if config.ProjectID == "" {
	//	return errors.New("ProjectID has not been set")
	// }
	return nil
}
