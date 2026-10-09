// Command server runs the fixture app's HTTP API.
//
//	go run ./cmd/server -db app.db -addr :8080
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	app "example.com/fixtures"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	path := flag.String("db", "app.db", "SQLite database file")
	flag.Parse()

	db, err := store.Open(context.Background(), *path, app.Schema)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, Routes(db)))
}
