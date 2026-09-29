// Command topo-server runs the offline simulation API for the network
// topology design platform. It never connects to a real network.
package main

import (
	"log"
	"net/http"
	"os"

	"topoplatform/topo"
)

func main() {
	addr := os.Getenv("TOPO_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := topo.NewServer()
	log.Printf("topology simulation API listening on %s (no real network access)", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
