package main

import (
	"log"
	"net/http"
	"os"

	"github.com/obzva/image-server/server"
)

func main() {
	gcs, err := server.NewGoogleCloudStorage()
	if err != nil {
		log.Fatal(err)
	}

	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = "3333"
	}

	s := server.NewServer(gcs)
	log.Printf("listening on port %s", port)
	if err := http.ListenAndServe(":"+port, s); err != nil {
		log.Fatal(err)
	}
}
