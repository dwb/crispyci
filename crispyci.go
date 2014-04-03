package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"
)

var (
	workingDir = flag.String("workingDir", "/var/lib/crispyci",
		"Working directory. Must be writable.")
	scriptDir = flag.String("scriptDir", "/usr/libexec/crispyci",
		"Where build scripts are kept. Can be read-only.")
	listenAddr = flag.String("httpAddr", "127.0.0.1:3000",
		"Address on which the HTTP interface should listen")
)

func main() {
	flag.Parse()

	os.Exit(func() int {
		if _, err := os.Stat(*workingDir); os.IsNotExist(err) {
			err = os.MkdirAll(path.Join(*workingDir, "db"), 0750|os.ModeDir)
			if err != nil {
				log.Print(err)
				return 1
			}
		}

		store := new(LevelDbStore)
		err := store.Init(path.Join(*workingDir, "db/crispyci.leveldb"))
		if err != nil {
			log.Print(err)
			return 1
		}
		defer store.Close()

		s, err := NewServer(store, *scriptDir, *workingDir, *listenAddr)
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
