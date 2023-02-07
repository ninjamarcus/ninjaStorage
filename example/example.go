package main

import (
	"crypto/md5"
	"flag"
	"fmt"
	"path"

	"github.com/ninjamarcus/ninjaStorage"
	ninjaStorageInterfaces "github.com/ninjamarcus/ninjaStorage/Interfaces"
	"github.com/ninjamarcus/ninjaStorage/gcpFS"
	"github.com/ninjamarcus/ninjaStorage/localFS"
	"github.com/ninjamarcus/ninjaStorage/models"
)

func localSetup() *localFS.LocalFS {
	conf := &models.LocalFSConfig{
		FS: &models.FS{ParentFolder: "/tmp/backup/dev"},
	}
	store, err := ninjaStorage.NewStorageLocal(conf)
	if err != nil {
		panic(fmt.Sprintf("failed to connect to storage: %v", err))
	}
	return store
}

func gcpSetup() *gcpFS.GCPFS {
	conf := &models.GCPFSConfig{
		BucketName: "marks-test-backup-bucket",
		ProjectID:  "not needed",
		FS:         &models.FS{ParentFolder: "backup/dev"},
	}
	store, err := ninjaStorage.NewStorageGCP(conf)
	if err != nil {
		panic(fmt.Sprintf("failed to connect to storage: %v", err))
	}
	return store
}

func main() {
	var local bool
	flag.BoolVar(&local, "local", false, "use local filesystem as storage")
	flag.Parse()

	//Setup
	var store ninjaStorageInterfaces.FileOperations
	if local {
		store = localSetup()
	} else {
		store = gcpSetup()
	}

	b := []byte("hello world")
	filePath := "testdir/test.data"
	metaData := &models.FileMetaData{UserMetaData: map[string]string{"Test": "metadata"}}

	//Write
	mdata, err := store.Write(b, filePath, metaData)
	if err != nil {
		panic(fmt.Sprintf("cannot write data to bucket: %v", err))
	}
	fmt.Printf("Written date: %s \n", mdata.TimeCreated)

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
		fmt.Printf("LIST: %v = %v\n", key, value)
	}

	//Delete
	if err = store.Delete(copyFilePath); err != nil {
		panic(fmt.Sprintf("cannot delete data from bucket: %v", err))
	}
}
