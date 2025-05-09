package envvar

import (
	"errors"
	"os"
)

const bucketNameEnvKey = "S3_BUCKET_NAME"

type EnvVar struct {
	BucketName string
}

func New() (*EnvVar, error) {
	bucketName := os.Getenv(bucketNameEnvKey)
	if bucketName == "" {
		return nil, errors.New("bucket name is required")
	}

	return &EnvVar{
		BucketName: bucketName,
	}, nil
}
