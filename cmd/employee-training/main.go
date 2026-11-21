package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"employee-training/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	server := &http.Server{Addr: *addr, Handler: web.New(), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Listening on http://%s", *addr)
	log.Fatal(server.ListenAndServe())
}
