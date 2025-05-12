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
	"regexp"
	"strconv"
	"strings"

	"github.com/disintegration/gift"
	"github.com/obzva/image-server/internal/envvar"
	"github.com/obzva/image-server/internal/storage"
)

const (
	errStrInvalidImagePath = "invalid image path"

	queryWidth  = "w"
	queryHeight = "h"
)

var (
	imagePathRegex = regexp.MustCompile(`^[^/]+\.(jpeg|jpg|png)$`)
)

func handler(logger *slog.Logger, storageClient storage.Client, envVar *envvar.EnvVar) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// check image path
		path := r.PathValue(slug)
		if !imagePathRegex.MatchString(path) {
			http.Error(w, errStrInvalidImagePath, http.StatusBadRequest)
			return
		}
		splitPath := strings.Split(path, ".")
		imageName := splitPath[0]
		imageFormat := splitPath[1]

		// check if this image exists
		originalKey := filepath.Join(envVar.FolderOriginal, path)
		originalOK, err := storageClient.CheckObject(r.Context(), originalKey)
		if err != nil {
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if !originalOK {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}

		width := 0
		height := 0

		// check query params: w & h
		q := r.URL.Query()
		if q.Has(queryWidth) {
			qWidth, err := strconv.Atoi(q.Get(queryWidth))
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
		if q.Has(queryHeight) {
			qHeight, err := strconv.Atoi(q.Get(queryHeight))
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
			http.Redirect(w, r, storageClient.ObjectURL(originalKey), http.StatusSeeOther)
			return
		}

		// check if resized image already exists
		resizedKey := filepath.Join(envVar.FolderResized, imageName, fmt.Sprintf("w%dh%d.%s", width, height, imageFormat))
		resizedOK, err := storageClient.CheckObject(r.Context(), resizedKey)
		if err != nil {
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// if resized image already exists
		if resizedOK {
			http.Redirect(w, r, storageClient.ObjectURL(resizedKey), http.StatusSeeOther)
			return
		}

		// else, let's resize it and upload it
		// first download the original image
		body, contentType, err := storageClient.DownloadObject(r.Context(), originalKey)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			if errors.Is(err, storage.ErrForbidden) {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer body.Close()

		// make it image.Image
		src, format, err := image.Decode(body)
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
		err = storageClient.UploadObject(r.Context(), resizedKey, &buf, contentType)
		if err != nil {
			if errors.Is(err, storage.ErrBadRequest) {
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			logger.Error(err.Error())
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// redirect to the new resized image
		http.Redirect(w, r, storageClient.ObjectURL(resizedKey), http.StatusSeeOther)
	}
}
