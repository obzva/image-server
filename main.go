package main

import (
	"log"
	"net/http"

	"github.com/obzva/image-server/server"
)

func main() {
	gcs, err := server.NewGoogleCloudStorage()
	if err != nil {
		log.Fatal(err)
	}
	
	s := server.NewServer(gcs)
	if err := http.ListenAndServe(":3333", s); err != nil {
		log.Fatal(err)
	}
}
