package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
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

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		log.Println("Exit requested; stopping after builds...")
		s.Stop()
	}()

	s.Serve()
}
