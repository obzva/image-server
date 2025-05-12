package server

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/neilotoole/slogt"
	"github.com/obzva/image-server/internal/envvar"
	"github.com/obzva/image-server/internal/storage"
)

type stubImageBody struct {
	*bytes.Buffer
}

func (sib *stubImageBody) Close() error {
	return nil
}

type stubObject struct {
	body        io.ReadCloser
	contentType string
}

func newStubObject(format string, width, height int) stubObject {
	var b bytes.Buffer
	sib := &stubImageBody{
		Buffer: &b,
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	switch format {
	case "jpeg":
		if err := jpeg.Encode(sib, img, nil); err != nil {
			log.Fatal(err)
		}
	case "png":
		if err := png.Encode(sib, img); err != nil {
			log.Fatal(err)
		}
	}

	return stubObject{
		body:        sib,
		contentType: "image/" + format,
	}
}

type stubStorageClient struct {
	storage    map[string]stubObject
	bucketName string
	execution  map[string]bool
}

const (
	exeKeyCheck    = "check"
	exeKeyDownload = "download"
	exeKeyUpload   = "upload"
)

func newStubStorageClient(envVar *envvar.EnvVar) *stubStorageClient {
	ssc := &stubStorageClient{
		storage:    make(map[string]stubObject),
		bucketName: envVar.BucketName,
		execution:  make(map[string]bool),
	}

	ssc.execution[exeKeyCheck] = false
	ssc.execution[exeKeyDownload] = false
	ssc.execution[exeKeyUpload] = false

	ssc.storage[filepath.Join(envVar.FolderOriginal, "imageJPEG.jpeg")] = newStubObject("jpeg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imageJPEG-2.jpeg")] = newStubObject("jpeg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imageJPEG-3.jpeg")] = newStubObject("jpeg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderResized, "imageJPEG", "w600h900.jpeg")] = newStubObject("jpeg", 600, 900)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imageJPG.jpg")] = newStubObject("jpeg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imageJPG-2.jpg")] = newStubObject("jpeg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imageJPG-3.jpg")] = newStubObject("jpeg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderResized, "imageJPG", "w600h900.jpg")] = newStubObject("jpeg", 600, 900)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imagePNG.png")] = newStubObject("png", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imagePNG-2.png")] = newStubObject("png", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "imagePNG-3.png")] = newStubObject("png", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderResized, "imagePNG", "w600h900.png")] = newStubObject("png", 600, 900)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "ratioJPEG.jpeg")] = newStubObject("jpeg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderResized, "ratioJPEG", "w600h0.jpeg")] = newStubObject("jpeg", 600, 600)
	ssc.storage[filepath.Join(envVar.FolderResized, "ratioJPEG", "w0h600.jpeg")] = newStubObject("jpeg", 600, 600)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "ratioJPG.jpg")] = newStubObject("jpg", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderResized, "ratioJPG", "w600h0.jpg")] = newStubObject("jpg", 600, 600)
	ssc.storage[filepath.Join(envVar.FolderResized, "ratioJPG", "w0h600.jpg")] = newStubObject("jpg", 600, 600)
	ssc.storage[filepath.Join(envVar.FolderOriginal, "ratioPNG.png")] = newStubObject("png", 300, 300)
	ssc.storage[filepath.Join(envVar.FolderResized, "ratioPNG", "w600h0.png")] = newStubObject("png", 600, 600)
	ssc.storage[filepath.Join(envVar.FolderResized, "ratioPNG", "w0h600.png")] = newStubObject("png", 600, 600)
	return ssc
}

func (sc *stubStorageClient) ObjectURL(objectKey string) string {
	return "https://test.test/" + filepath.Join(sc.bucketName, objectKey)
}

func (sc *stubStorageClient) CheckObject(ctx context.Context, objectKey string) (bool, error) {
	sc.execution[exeKeyCheck] = true
	_, ok := sc.storage[objectKey]
	if !ok {
		return false, nil
	}
	return true, nil
}

func (sc *stubStorageClient) DownloadObject(ctx context.Context, objectKey string) (body io.ReadCloser, contentType string, err error) {
	sc.execution[exeKeyDownload] = true
	object, ok := sc.storage[objectKey]
	if !ok {
		return nil, "", storage.ErrNotFound
	}
	return object.body, object.contentType, nil
}

func (sc *stubStorageClient) UploadObject(ctx context.Context, objectKey string, body io.Reader, contentType string) error {
	sc.execution[exeKeyUpload] = true
	img, format, err := image.Decode(body)
	if err != nil {
		return err
	}
	sc.storage[objectKey] = newStubObject(format, img.Bounds().Dx(), img.Bounds().Dy())
	return nil
}

func TestHandler(t *testing.T) {
	// stub logger
	sl := slogt.New(t, slogt.Factory(func(w io.Writer) slog.Handler {
		return slog.NewTextHandler(w, &slog.HandlerOptions{
			AddSource: true,
		})
	}))

	// stub env var
	sev := &envvar.EnvVar{
		BucketName:     "stub-bucket",
		FolderOriginal: "stub-original-folder",
		FolderResized:  "stub-resized-folder",
	}

	// stub storage client
	ssc := newStubStorageClient(sev)

	// stub server
	ss := New(sl, ssc, sev)

	tt := []struct {
		testName  string
		imageSlug string
		// desired response status code
		statusCode int
		// desired response body
		body string
		// desired result image dimensions
		width  int
		height int
		// desired Location header of redirection
		location string
		// check executions
		executions []string
	}{
		{
			testName:   "check invalid image path",
			imageSlug:  "asdf",
			statusCode: http.StatusBadRequest,
			body:       errStrInvalidImagePath,
		},
		{
			testName:   "only jpeg and png formats are available",
			imageSlug:  "random.rand",
			statusCode: http.StatusBadRequest,
			body:       errStrInvalidImagePath,
		},
		{
			testName:   "image doesn't exist",
			imageSlug:  "noexist.jpeg",
			statusCode: http.StatusNotFound,
			body:       http.StatusText(http.StatusNotFound),
		},
		{
			testName:   "redirect to original jpeg image",
			imageSlug:  "imageJPEG.jpeg",
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderOriginal, "imageJPEG.jpeg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to original jpg image",
			imageSlug:  "imageJPG.jpg",
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderOriginal, "imageJPG.jpg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to original png image",
			imageSlug:  "imagePNG.png",
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderOriginal, "imagePNG.png"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized jpeg image",
			imageSlug:  "imageJPEG.jpeg",
			width:      600,
			height:     900,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPEG", "w600h900.jpeg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized jpg image",
			imageSlug:  "imageJPG.jpg",
			width:      600,
			height:     900,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPG", "w600h900.jpg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized png image",
			imageSlug:  "imagePNG.png",
			width:      600,
			height:     900,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imagePNG", "w600h900.png"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized jpeg image without height query",
			imageSlug:  "ratioJPEG.jpeg",
			width:      600,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "ratioJPEG", "w600h0.jpeg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized jpg image without height query",
			imageSlug:  "ratioJPG.jpg",
			width:      600,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "ratioJPG", "w600h0.jpg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized png image without height query",
			imageSlug:  "ratioPNG.png",
			width:      600,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "ratioPNG", "w600h0.png"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized jpeg image without width query",
			imageSlug:  "ratioJPEG.jpeg",
			height:     600,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "ratioJPEG", "w0h600.jpeg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized jpg image without width query",
			imageSlug:  "ratioJPG.jpg",
			height:     600,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "ratioJPG", "w0h600.jpg"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "redirect to already-resized png image without width query",
			imageSlug:  "ratioPNG.png",
			height:     600,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "ratioPNG", "w0h600.png"),
			executions: []string{exeKeyCheck},
		},
		{
			testName:   "resize the original image and redirect to the resized jpeg image without height query",
			imageSlug:  "imageJPEG.jpeg",
			width:      1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPEG", "w1200h0.jpeg"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:   "resize the original image and redirect to the resized jpg image without height query",
			imageSlug:  "imageJPG.jpg",
			width:      1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPG", "w1200h0.jpg"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:   "resize the original image and redirect to the resized png image without height query",
			imageSlug:  "imagePNG.png",
			width:      1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imagePNG", "w1200h0.png"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:   "resize the original image and redirect to the resized jpeg image without width query",
			imageSlug:  "imageJPEG-2.jpeg",
			height:     1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPEG-2", "w0h1200.jpeg"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:  "resize the original image and redirect to the resized jpg image without width query",
			imageSlug: "imageJPG-2.jpg",
			height:    1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPG-2", "w0h1200.jpg"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:  "resize the original image and redirect to the resized png image without width query",
			imageSlug: "imagePNG-2.png",
			height:    1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imagePNG-2", "w0h1200.png"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:   "resize the original image and redirect to the resized jpeg image",
			imageSlug:  "imageJPEG-3.jpeg",
			width: 900,
			height:     1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPEG-3", "w900h1200.jpeg"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:  "resize the original image and redirect to the resized jpg image",
			imageSlug: "imageJPG-3.jpg",
			width: 900,
			height:    1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imageJPG-3", "w900h1200.jpg"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
		{
			testName:  "resize the original image and redirect to the resized png image",
			imageSlug: "imagePNG-3.png",
			width: 900,
			height:    1200,
			location:   "https://test.test/" + filepath.Join(sev.BucketName, sev.FolderResized, "imagePNG-3", "w900h1200.png"),
			executions: []string{exeKeyCheck, exeKeyDownload, exeKeyUpload},
		},
	}

	for _, tc := range tt {
		t.Run(tc.testName, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/"+tc.imageSlug, nil)
			if tc.width != 0 || tc.height != 0 {
				q := req.URL.Query()
				if tc.width != 0 {
					q.Add("w", strconv.Itoa(tc.width))
				}
				if tc.height != 0 {
					q.Add("h", strconv.Itoa(tc.height))
				}
				req.URL.RawQuery = q.Encode()
			}

			ss.ServeHTTP(rr, req)

			res := rr.Result()
			defer res.Body.Close()

			if tc.statusCode != 0 {
				// check status code
				assertEqual(t, res.StatusCode, tc.statusCode)
			}

			if tc.body != "" {
				// check response body
				body, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				assertEqual(t, strings.TrimSpace(string(body)), tc.body)
			}

			if tc.location != "" {
				// check redirection
				assertEqual(t, res.StatusCode, http.StatusSeeOther)
				assertEqual(t, res.Header.Get("Location"), tc.location)
			}

			// check execution of methods
			if len(tc.executions) != 0 {
				for _, e := range []string{exeKeyCheck, exeKeyUpload, exeKeyDownload} {
					if slices.Contains(tc.executions, e) {
						if e == exeKeyUpload {
							splitSlug := strings.Split(tc.imageSlug, ".")
							resizedKey := filepath.Join(sev.FolderResized, splitSlug[0], fmt.Sprintf("w%dh%d.%s", tc.width, tc.height, splitSlug[1]))
							_, ok := ssc.storage[resizedKey]
							assertEqual(t, ok, true)
						}
						assertEqual(t, ssc.execution[e], true)
					} else {
						assertEqual(t, ssc.execution[e], false)
					}
				}
			}
		})
	}
}

func assertEqual[U comparable](t *testing.T, got, want U) {
	t.Helper()
	if got != want {
		t.Errorf("got %v; want %v", got, want)
	}
}
