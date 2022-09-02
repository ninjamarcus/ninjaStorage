package main

import (
	"crypto/md5"
	"fmt"
	"github.com/ninjamarcus/ninjaStorage"
	"github.com/ninjamarcus/ninjaStorage/models"
	"path"
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
	b := []byte("hello world")
	filePath := "testdir/test.data"
	//Write
	metaData := &models.FileMetaData{UserMetaData: map[string]string{"Test": "metadata"}}

	mdata, err := store.Write(b, filePath, metaData)
	if err != nil {
		panic(fmt.Sprintf("cannot write data to bucket: %v", err))
	}
	fmt.Printf("Written md5sum: %s \n", mdata.Md5Hash)

	//Read
	data, mdata, err := store.Read(filePath)
	fmt.Printf("read filename From Metadata: %s \n", mdata.Name)
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
