package server

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/obzva/gato"
)

type ImageStorage interface {
	GetImageReader(ctx context.Context, name string) (io.ReadCloser, error)
	SaveImage(ctx context.Context, name string, img *image.RGBA) error
}

type Server struct {
	storage ImageStorage
}

func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// validate image name and extract format
	imgName := strings.TrimPrefix(r.URL.Path, "/images/")

	imgNamePart, imgFormatPart, err := splitImageName(imgName)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	if imgFormatPart != "jpg" && imgFormatPart != "jpeg" && imgFormatPart != "png" {
		http.Error(rw, "invalid image format", http.StatusBadRequest)
		return
	}

	// get w and h from query params
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

	// check if processed image exists
	processedImgName := fmt.Sprintf("processed/%s-w%d-h%d.%s", imgNamePart, w, h, imgFormatPart)
	rc, err := s.storage.GetImageReader(r.Context(), processedImgName)
	switch {
	case err == nil:
		// if processed one exists, we can use this one
		defer rc.Close()
		if _, err := io.Copy(rw, rc); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
	case errors.Is(err, storage.ErrObjectNotExist):
		// if processed one doesn't exist, we have to get original image from storage
		rc, err = s.storage.GetImageReader(r.Context(), imgName)
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

		// set w and h for gato.Instruction
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

		// if saveNewOne is true, save processed image to storage
		saveName := fmt.Sprintf("processed/%s-w%d-h%d.%s", imgNamePart, w, h, imgFormatPart)
		if err := s.storage.SaveImage(r.Context(), saveName, dstImg); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		// write response
		if err := writeImage(rw, dstImg, imgFormatPart); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

	default:
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
}

func NewServer(s ImageStorage) *Server {
	return &Server{s}
}

type GoogleCloudStorage struct {
	BucketName string
}

func (gcs *GoogleCloudStorage) GetImageReader(ctx context.Context, name string) (io.ReadCloser, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage client: %w", err)
	}
	defer client.Close()

	bkt := client.Bucket(gcs.BucketName)
	rc, err := bkt.Object(name).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, storage.ErrObjectNotExist
		}
		return nil, fmt.Errorf("failed to read image %s: %w", name, err)
	}

	return rc, nil
}

func (gcs *GoogleCloudStorage) SaveImage(ctx context.Context, name string, img *image.RGBA) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize storage client: %w", err)
	}
	defer client.Close()

	_, imgFormat, err := splitImageName(name)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*50)
	defer cancel()

	o := client.Bucket(gcs.BucketName).Object(name)
	wc := o.If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	if err := writeImage(wc, img, imgFormat); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	return nil

}

func NewGoogleCloudStorage() (*GoogleCloudStorage, error) {
	bktName, ok := os.LookupEnv("GCS_BUCKET_NAME")
	if !ok {
		return nil, fmt.Errorf("GCS_BUCKET_NAME is not set")
	}

	return &GoogleCloudStorage{bktName}, nil
}

func splitImageName(name string) (namePart string, formatPart string, err error) {
	re := regexp.MustCompile(`^(.+)\.([^.]+)$`)

	matches := re.FindStringSubmatch(name)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("invalid image name: %s", name)
	}

	return matches[1], matches[2], nil
}

func writeImage(w io.Writer, img *image.RGBA, imgFormat string) error {
	switch imgFormat {
	case "jpg", "jpeg":
		if err := jpeg.Encode(w, img, nil); err != nil {
			return fmt.Errorf("failed to write image: %w", err)
		}
	case "png":
		if err := png.Encode(w, img); err != nil {
			return fmt.Errorf("failed to write image: %w", err)
		}
	}
	return nil
}
