package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/obzva/image-server/internal/envvar"
	"github.com/obzva/image-server/internal/storage"
)

const slug = "image"

func New(logger *slog.Logger, storageClient storage.Client, envVar *envvar.EnvVar) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("GET /{%s}", slug), handler(logger, storageClient, envVar))

	return mux
}
