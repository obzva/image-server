package server

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloud.google.com/go/storage"
)

// Tests

func TestGETImages(t *testing.T) {
	stubJPEG := newStubJPEG()
	stubPNG := newStubPNG()
	stubJPEGReader := newStubImageReader(stubJPEG)
	stubPNGReader := newStubImageReader(stubPNG)
	stubStore := &stubImageStorage{
		images: map[string]io.ReadCloser{
			"norwich-terrier.jpg": stubJPEGReader,
			"orange-cat.png":      stubPNGReader,
		},
	}
	stubServer := NewServer(stubStore)

	// path tests
	t.Run("return bad request when image name is invalid", func(t *testing.T) {
		request := newGetImageRequest("invalid-image-name", "")
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		assertResponseStatusCode(t, response.Result().StatusCode, http.StatusBadRequest)
	})

	t.Run("return bad request when image format is invalid", func(t *testing.T) {
		request := newGetImageRequest("norwich-terrier.jpg.gif", "")
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		assertResponseStatusCode(t, response.Result().StatusCode, http.StatusBadRequest)
	})

	// storage tests
	t.Run("return image data when image exists", func(t *testing.T) {
		request := newGetImageRequest("norwich-terrier.jpg", "")
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		assertBytes(t, response.Body.Bytes(), stubJPEG)
	})

	t.Run("return not found when image does not exist", func(t *testing.T) {
		request := newGetImageRequest("orange-cat.jpg", "")
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		assertResponseStatusCode(t, response.Result().StatusCode, http.StatusNotFound)
	})

	// size tests
	t.Run("return right sized image when w and h are provided", func(t *testing.T) {
		w, h := 200, 300
		request := newGetImageRequest("norwich-terrier.jpg", fmt.Sprintf("w=%d&h=%d", w, h))
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		assertImageSize(t, response.Body.Bytes(), struct{ w, h int }{w, h})
	})

	t.Run("return right sized (keeping aspect ratio) image when one of w or h is omitted", func(t *testing.T) {
		d := 200
		request := newGetImageRequest("norwich-terrier.jpg", fmt.Sprintf("w=%d", d))
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		assertImageSize(t, response.Body.Bytes(), struct{ w, h int }{d, d})

		request = newGetImageRequest("norwich-terrier.jpg", fmt.Sprintf("h=%d", d))
		response = httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		assertImageSize(t, response.Body.Bytes(), struct{ w, h int }{d, d})
	})

	// format tests
	t.Run("return jpeg image when image is jpeg", func(t *testing.T) {
		request := newGetImageRequest("norwich-terrier.jpg", "")
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		got := response.Header().Get("Content-Type")
		want := "image/jpeg"

		assertString(t, got, want)
	})

	t.Run("return png image when image is png", func(t *testing.T) {
		request := newGetImageRequest("orange-cat.png", "")
		response := httptest.NewRecorder()

		stubServer.ServeHTTP(response, request)

		got := response.Header().Get("Content-Type")
		want := "image/png"

		assertString(t, got, want)
	})
}

// Stub helpers

type stubImageStorage struct {
	images map[string]io.ReadCloser
}

func (s *stubImageStorage) GetImageReader(ctx context.Context, name string) (io.ReadCloser, error) {
	imgData, ok := s.images[name]
	if !ok {
		return nil, storage.ErrObjectNotExist
	}
	return imgData, nil
}

type stubImageReader struct {
	image []byte
}

func (s *stubImageReader) Read(p []byte) (int, error) {
	n := copy(p, s.image)
	if n == len(s.image) {
		return n, io.EOF
	}
	return n, nil
}

func (s *stubImageReader) Close() error {
	return nil
}

func newStubImageReader(d []byte) *stubImageReader {
	return &stubImageReader{image: d}
}

func newStubJPEG() []byte {
	mockImg := image.NewRGBA(image.Rect(0, 0, 100, 100))
	b := new(bytes.Buffer)
	_ = jpeg.Encode(b, mockImg, nil)
	return b.Bytes()
}

func newStubPNG() []byte {
	mockImg := image.NewRGBA(image.Rect(0, 0, 100, 100))
	b := new(bytes.Buffer)
	_ = png.Encode(b, mockImg)
	return b.Bytes()
}

// General helpers

func newGetImageRequest(imgName, query string) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/images/%s?%s", imgName, query), nil)
	return request
}

func assertBytes(t testing.TB, got, want []byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Errorf("response body is wrong, got %d, want %d", got, want)
	}
}

func assertResponseStatusCode(t testing.TB, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("response status code is wrong, got %d, want %d", got, want)
	}
}

func assertImageSize(t testing.TB, got []byte, want struct{ w, h int }) {
	t.Helper()

	img, _, err := image.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("failed to decode image: %v", err)
	}

	if img.Bounds().Dx() != want.w || img.Bounds().Dy() != want.h {
		t.Errorf("image size is wrong, got w: %d, h: %d, want w: %d, h: %d", img.Bounds().Dx(), img.Bounds().Dy(), want.w, want.h)
	}
}

func assertString(t testing.TB, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
