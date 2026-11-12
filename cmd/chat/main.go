package main

import (
	"log"
	"net/http"
	"time"

	"chat/internal/httpapi"
)

func main() {
	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           httpapi.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("starting HTTP server on %s", server.Addr)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
