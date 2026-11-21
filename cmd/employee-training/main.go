package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"employee-training/internal/store"
	"employee-training/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	dbPath := flag.String("db", "training.db", "SQLite database path")
	seed := flag.Bool("seed", false, "prepare demo accounts and exit")
	showcase := flag.Bool("seed-showcase", false, "prepare accounts and unlock the final exam for a fresh second employee")
	flag.Parse()
	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if *seed || *showcase {
		prepare := db.Seed
		if *showcase {
			prepare = db.SeedShowcase
		}
		if err := prepare(); err != nil {
			log.Fatal(err)
		}
		log.Print("Demo accounts ready")
		return
	}
	server := &http.Server{Addr: *addr, Handler: web.New(db), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Listening on http://%s", *addr)
	log.Fatal(server.ListenAndServe())
}
