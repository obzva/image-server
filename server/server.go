package server

import (
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/obzva/gato"
)

type ImageStorage interface {
	GetImageReader(ctx context.Context, name string) (io.ReadCloser, error)
}

type Server struct {
	storage ImageStorage
}

func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// validate image name and extract format
	imgName := strings.TrimPrefix(r.URL.Path, "/images/")

	re, err := regexp.Compile(`^(.+)\.([^.]+)$`)
	if err != nil {
		log.Fatal(err)
	}

	matches := re.FindStringSubmatch(imgName)
	if len(matches) != 3 {
		http.Error(rw, "invalid image name", http.StatusBadRequest)
		return
	}

	imgFormat := matches[2]
	if imgFormat == "jpg" {
		imgFormat = "jpeg"
	}
	if imgFormat != "jpeg" && imgFormat != "png" {
		http.Error(rw, "invalid image format", http.StatusBadRequest)
		return
	}

	// get image reader from storage
	rc, err := s.storage.GetImageReader(r.Context(), imgName)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, storage.ErrObjectNotExist) {
			statusCode = http.StatusNotFound
		}
		http.Error(rw, err.Error(), statusCode)
		return
	}
	defer rc.Close()

	// create gato.Data
	data, err := gato.NewData(imgName, rc)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	srcImg := data.Image

	// set w and h
	q := r.URL.Query()

	w := 0
	if qW := q.Get("w"); qW != "" {
		w, err = strconv.Atoi(qW)
		if err != nil {
			http.Error(rw, "invalid w", http.StatusBadRequest)
			return
		}
	}

	h := 0
	if qH := q.Get("h"); qH != "" {
		h, err = strconv.Atoi(qH)
		if err != nil {
			http.Error(rw, "invalid h", http.StatusBadRequest)
			return
		}
	}

	if w == 0 && h == 0 {
		w = srcImg.Bounds().Dx()
		h = srcImg.Bounds().Dy()
	}

	// create gato.Instruction
	ist := gato.Instruction{
		Width:         w,
		Height:        h,
		Interpolation: q.Get("m"),
	}

	// create gato.Processor
	prc, err := gato.NewProcessor(ist)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	// process image
	dstImg, err := prc.Process(data)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	// write response
	switch imgFormat {
	case "jpeg":
		if err := jpeg.Encode(rw, dstImg, nil); err != nil {
			http.Error(rw, "failed to write response", http.StatusInternalServerError)
			return
		}
	case "png":
		if err := png.Encode(rw, dstImg); err != nil {
			http.Error(rw, "failed to write response", http.StatusInternalServerError)
			return
		}
	}
}

func NewServer(s ImageStorage) *Server {
	return &Server{s}
}

type GoogleCloudStorage struct {
	BucketName string
}

func (gcs *GoogleCloudStorage) GetImageReader(ctx context.Context, objName string) (io.ReadCloser, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage client: %w", err)
	}
	defer client.Close()

	bkt := client.Bucket(gcs.BucketName)
	rc, err := bkt.Object(objName).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, storage.ErrObjectNotExist
		}
		return nil, fmt.Errorf("failed to read image %s: %w", objName, err)
	}

	return rc, nil
}

func NewGoogleCloudStorage() (*GoogleCloudStorage, error) {
	bktName, ok := os.LookupEnv("GCS_BUCKET_NAME")
	if !ok {
		return nil, fmt.Errorf("GCS_BUCKET_NAME is not set")
	}

	return &GoogleCloudStorage{bktName}, nil
}
