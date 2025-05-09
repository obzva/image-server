package server

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/disintegration/gift"
	"github.com/obzva/image-server/internal/envvar"
)

const (
	originalFolder = "original"
	resizedFolder  = "resized"

	widthQuery  = "w"
	heightQuery = "h"
)

func getImageHandler(logger *slog.Logger, s3Client *s3.Client, envVar *envvar.EnvVar) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("imageName")
		if imageName == "" {
			http.Error(w, "image name is required", http.StatusBadRequest)
			return
		}
		// check if this image exists
		originalKey := filepath.Join(originalFolder, imageName)
		_, err := s3Client.HeadObject(r.Context(), &s3.HeadObjectInput{
			Bucket: aws.String(envVar.BucketName),
			Key:    aws.String(originalKey),
		})
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == "NotFound" {
					http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
					return
				}
			}
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		width := 0
		height := 0

		// check query params: w & h
		q := r.URL.Query()
		if q.Has(widthQuery) {
			qWidth, err := strconv.Atoi(q.Get(widthQuery))
			if err != nil {
				http.Error(w, "failed converting w into integer", http.StatusBadRequest)
				return
			}
			if qWidth <= 0 {
				http.Error(w, "if specified, w must be larger than 0", http.StatusBadRequest)
				return
			}
			width = qWidth
		}
		if q.Has(heightQuery) {
			qHeight, err := strconv.Atoi(q.Get(heightQuery))
			if err != nil {
				http.Error(w, "failed converting h into integer", http.StatusBadRequest)
				return
			}
			if qHeight <= 0 {
				http.Error(w, "if specified, h must be larger than 0", http.StatusBadRequest)
				return
			}
			height = qHeight
		}

		// if they are requesting original image then redirect to S3 object URL
		if width == 0 && height == 0 {
			s3URL := fmt.Sprintf("https://%s.s3.ca-west-1.amazonaws.com/%s", envVar.BucketName, originalKey)
			http.Redirect(w, r, s3URL, http.StatusFound)
			return
		}

		// check if resized image already exists
		resizedExists := true
		splittedImageName := strings.Split(imageName, ".")
		resizedKey := filepath.Join(resizedFolder, splittedImageName[0], fmt.Sprintf("w%dh%d.%s", width, height, splittedImageName[1]))
		_, err = s3Client.HeadObject(r.Context(), &s3.HeadObjectInput{
			Bucket: aws.String(envVar.BucketName),
			Key:    aws.String(resizedKey),
		})
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) && ae.ErrorCode() == "NotFound" {
				resizedExists = false
			} else {
				logger.Error(err.Error())
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		// if resized image already exists
		if resizedExists {
			s3URL := fmt.Sprintf("https://%s.s3.ca-west-1.amazonaws.com/%s", envVar.BucketName, resizedKey)
			http.Redirect(w, r, s3URL, http.StatusFound)
			return
		}

		// else, let's resize it and upload it

		// first download the original image
		originalObject, err := s3Client.GetObject(r.Context(), &s3.GetObjectInput{
			Bucket: aws.String(envVar.BucketName),
			Key:    aws.String(originalKey),
		})
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				switch ae.ErrorCode() {
				case "NotFound":
					http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
					return
				case "InvalidObjectState":
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
			}
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer originalObject.Body.Close()

		// make it image.Image
		src, format, err := image.Decode(originalObject.Body)
		if err != nil {
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// resize image
		g := gift.New(gift.Resize(width, height, gift.LanczosResampling))
		dst := image.NewRGBA(g.Bounds(src.Bounds()))
		g.Draw(dst, src)
		var buf bytes.Buffer
		switch format {
		case "jpeg":
			err = jpeg.Encode(&buf, dst, nil)
			if err != nil {
				logger.Error(err.Error())
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		case "png":
			err = png.Encode(&buf, dst)
			if err != nil {
				logger.Error(err.Error())
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		// upload resized image
		_, err = s3Client.PutObject(r.Context(), &s3.PutObjectInput{
			Bucket:      aws.String(envVar.BucketName),
			Key:         aws.String(resizedKey),
			Body:        &buf,
			ContentType: originalObject.ContentType,
		})
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) && ae.ErrorCode() == "EntityTooLarge" {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// redirect to the new resized image
		s3URL := fmt.Sprintf("https://%s.s3.ca-west-1.amazonaws.com/%s", envVar.BucketName, resizedKey)
		http.Redirect(w, r, s3URL, http.StatusFound)
	}
}
