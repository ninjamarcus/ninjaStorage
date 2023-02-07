package models

import "time"

// TODO: for local ninjaStorage store the metadata in its own folder so we can search it super quickly-ish.

type FileMetaData struct {
	Bucket       string            `json:"bucket,omitempty"`
	Md5Hash      string            `json:"md_5_hash,omitempty"`
	UserMetaData map[string]string `json:"user_meta_data,omitempty"`
	Name         string            `json:"name,omitempty"`
	Size         int64             `json:"size,omitempty"`
	TimeCreated  time.Time         `json:"time_created,omitempty"`
	Updated      time.Time         `json:"updated,omitempty"`
}
