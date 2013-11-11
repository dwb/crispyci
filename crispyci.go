package main

import (
	"log"
	"os"
)

func main() {
	// TODO: command-line options
	err := os.Chdir("/var/lib/crispyci")
	if err != nil {
		log.Fatal(err)
	}

	s, err := NewServer()
	if err != nil {
		log.Fatal(err)
	}
	s.Serve()
}
