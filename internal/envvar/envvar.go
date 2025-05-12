package envvar

import (
	"fmt"
	"os"
)

const (
	bucketNameEnvKey     = "S3_BUCKET_NAME"
	envKeyFolderOriginal = "ORIGINAL_FOLDER"
	envKeyFolderResized  = "RESIZED_FOLDER"
)

type EnvVar struct {
	BucketName     string
	FolderOriginal string
	FolderResized  string
}

func New() (*EnvVar, error) {
	bucketName, err := checkKey(bucketNameEnvKey)
	if err != nil {
		return nil, err
	}
	folderOriginal, err := checkKey(envKeyFolderOriginal)
	if err != nil {
		return nil, err
	}
	folderResized, err := checkKey(envKeyFolderResized)
	if err != nil {
		return nil, err
	}

	return &EnvVar{
		BucketName:     bucketName,
		FolderOriginal: folderOriginal,
		FolderResized:  folderResized,
	}, nil
}

func checkKey(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("env var %q is required", key)
	}
	return value, nil
}
