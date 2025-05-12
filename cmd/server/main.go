package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/obzva/image-server/internal/envvar"
	"github.com/obzva/image-server/internal/server"
	"github.com/obzva/image-server/internal/storage"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))

	envVar, err := envvar.New()
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	s3Client, err := storage.NewS3Client(envVar.BucketName)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	srv := server.New(logger, s3Client, envVar)

	s := http.Server{
		Handler: srv,
		Addr:    ":3000",
	}

	if err := s.ListenAndServe(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
