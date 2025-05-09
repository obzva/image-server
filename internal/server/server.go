package server

import (
	"log/slog"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/obzva/image-server/internal/envvar"
)

func New(logger *slog.Logger, s3Client *s3.Client, envVar *envvar.EnvVar) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{imageName}", getImageHandler(logger, s3Client, envVar))

	return mux
}
