package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// TODO: command-line options

	s, err := NewServer("/usr/libexec/crispyci", "/var/lib/crispyci", "127.0.0.1:3000")
	if err != nil {
		log.Fatal(err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		log.Println("Exit requested; stopping after builds...")
		s.Stop()
	}()

	s.Serve()
}
