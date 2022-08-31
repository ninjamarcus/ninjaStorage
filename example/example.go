package main

import (
	"crypto/md5"
	"fmt"
	"path"
	ninjaStorage "storage"
	"storage/models"
)

func main() {
	//Setup
	conf := &models.GCPFSConfig{
		BucketName: "marks-test-backup-bucket",
		ProjectID:  "not needed",
		FS:         &models.FS{ParentFolder: "backup/dev"},
	}
	store, err := ninjaStorage.NewStorageGCP(conf)
	if err != nil {
		panic(fmt.Sprintf("failed to connect to bucket: %v", err))
	}
	//Write
	b := []byte("hello world")
	filePath := "testdir/test.data"
	metaData := &models.FileMetaData{UserMetaData: map[string]string{"Test": "metadata"}}
	if err = store.Write(b, filePath, metaData); err != nil {
		panic(fmt.Sprintf("cannot write data to bucket: %v", err))
	}
	//Read
	data, err := store.Read(filePath)
	if err != nil {
		panic(fmt.Sprintf("cannot read data from bucket: %v", err))
	}
	if md5.Sum(data) != md5.Sum(b) {
		panic(fmt.Sprintf("the data written is not the same as data read"))
	}

	//Copy
	copyFilePath := path.Join("newdir", filePath)
	if err = store.Copy(filePath, copyFilePath); err != nil {
		panic(fmt.Sprintf("cannot copy data in bucket: %v", err))
	}

	//List
	result, err := store.List(filePath)
	if err != nil {
		panic(fmt.Sprintf("cannot retrieve data from bucket: %v", err))
	}
	
	for key, value := range result {
		fmt.Printf("\t %v = %v\n", key, value)
	}
	//Delete
	if err = store.Delete(copyFilePath); err != nil {
		panic(fmt.Sprintf("cannot delete data from bucket: %v", err))
	}

}
