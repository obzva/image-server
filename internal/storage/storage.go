package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

var (
	ErrNotFound   = errors.New(http.StatusText(http.StatusNotFound))
	ErrForbidden  = errors.New(http.StatusText(http.StatusForbidden))
	ErrBadRequest = errors.New(http.StatusText(http.StatusBadRequest))
)

type Client interface {
	ObjectURL(objectKey string) string

	CheckObject(ctx context.Context, objectKey string) (bool, error)
	DownloadObject(ctx context.Context, objectKey string) (body io.ReadCloser, contentType string, err error)
	UploadObject(ctx context.Context, objectKey string, body io.Reader, contentType string) error
}

type S3Client struct {
	client     *s3.Client
	bucketName string
}

func NewS3Client(bucketName string) (*S3Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return &S3Client{
		client:     s3.NewFromConfig(cfg),
		bucketName: bucketName,
	}, nil
}

func (sc *S3Client) ObjectURL(objectKey string) string {
	s3URLFormat := "https://%s.s3.ca-west-1.amazonaws.com/%s"
	return fmt.Sprintf(s3URLFormat, sc.bucketName, objectKey)
}

func (sc *S3Client) CheckObject(ctx context.Context, objectKey string) (bool, error) {
	_, err := sc.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(sc.bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var re *smithyhttp.ResponseError
		if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (sc *S3Client) DownloadObject(ctx context.Context, objectKey string) (io.ReadCloser, string, error) {
	object, err := sc.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(sc.bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var re *smithyhttp.ResponseError
		if errors.As(err, &re) {
			switch re.HTTPStatusCode() {
			case http.StatusNotFound:
				return nil, "", ErrNotFound
			case http.StatusForbidden:
				return nil, "", ErrForbidden
			}
		}
		return nil, "", err
	}
	return object.Body, *object.ContentType, nil
}

func (sc *S3Client) UploadObject(ctx context.Context, objectKey string, body io.Reader, contentType string) error {
	_, err := sc.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(sc.bucketName),
		Key:         aws.String(objectKey),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		var re *smithyhttp.ResponseError
		if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusBadRequest {
			return ErrBadRequest
		}
		return err
	}
	return nil
}
