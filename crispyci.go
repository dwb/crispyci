package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// TODO: command-line options

	os.Exit(func() int {
		store := new(LevelDbStore)
		err := store.Init("/var/lib/crispyci/db/crispyci.leveldb")
		if err != nil {
			log.Print(err)
			return 1
		}
		defer store.Close()

		s, err := NewServer(store, "/usr/libexec/crispyci", "/var/lib/crispyci",
			"127.0.0.1:3000")
		if err != nil {
			log.Print(err)
			return 1
		}

		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-signals
			log.Println("Exit requested; stopping after builds...")
			s.Stop()
		}()

		s.Serve()
		return 0
	}())
}
