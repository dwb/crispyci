package main

import (
	"container/list"
	"fmt"
	"log"
	"os"
)

const MaxConcurrentJobs = 5

func main() {
	var store Store

	// TODO: command-line options
	err := os.Chdir("/var/lib/crispyci")
	if err != nil {
		log.Fatal(err)
	}

	// TODO: a useful store!
	store = new(StaticStore)
	store.Init("")
	runJobChan := make(chan JobRunRequest, 1)

	go func() {
		httpServer := NewHttpInterface(runJobChan)
		httpServer.Addr = "127.0.0.1:3000"
		log.Printf("Listening on %s ...", httpServer.Addr)
		httpServer.ListenAndServe()
	}()

	concurrentJobTokens := make(chan bool, MaxConcurrentJobs)
	// Pre-fill with start "tokens"
	for i := 0; i < cap(concurrentJobTokens); i++ {
		concurrentJobTokens <- true
	}

	// TODO: refactor this main loop into struct
	runningJobs := make(map[JobId]*JobRun)
	canStartJobChan := make(chan JobRunRequest, 5)
	startedJobChan := make(chan JobRun, 5)
	finishedJobChan := make(chan JobResult, 5)
	waitingJobRuns := make(map[JobId]*list.List)

	runJobFromRequest := func(req JobRunRequest) {
		go runJobFromRequestWithChannels(req, store, canStartJobChan, concurrentJobTokens, startedJobChan, finishedJobChan)
	}

	log.Println("Accepting jobs...")
	for {
		select {
		case req := <-runJobChan:
			runJobFromRequest(req)

		case req := <-canStartJobChan:
			if _, ok := runningJobs[req.Job.Id]; ok {
				log.Printf("'%s' is already running; queueing", req.Job.Name)
				id := req.Job.Id
				jobQueue := waitingJobRuns[id]
				if jobQueue == nil {
					jobQueue = list.New()
					waitingJobRuns[id] = jobQueue
				}
				jobQueue.PushFront(req)
			} else {
				req.AllowStart <- true
			}

		case jobRun := <-startedJobChan:
			log.Printf("Started job: %s\n", jobRun.Job.Name)
			runningJobs[jobRun.Job.Id] = &jobRun

		case jobResult := <-finishedJobChan:
			log.Printf("Job finished: %s\n", jobResult.Job.Name)
			delete(runningJobs, jobResult.Job.Id)
			concurrentJobTokens <- true
			if jobQueue, ok := waitingJobRuns[jobResult.Job.Id]; ok {
				reqElement := jobQueue.Back()
				if reqElement != nil {
					req := jobQueue.Remove(reqElement).(JobRunRequest)
					runJobFromRequest(req)
				}
			}
		}
	}
}

func jobProgressNotifier() (chanOut chan JobProgress) {
	// TODO: this will need to come out to multiple consumers
	chanOut = make(chan JobProgress, 1)
	go func() {
		for progress := range chanOut {
			fmt.Print(progress.Line)
		}
	}()
	return
}

func runJobFromRequestWithChannels(req JobRunRequest, store Store, canStartJobChan chan JobRunRequest, concurrentJobTokens chan bool, startedJobChan chan JobRun, finishedJobChan chan JobResult) {
	log.Printf("Received job run request for '%s'\n", req.JobName)

	job, err := req.FindJob(store)
	if job == nil {
		log.Printf("Couldn't find job '%s'\n", req.JobName)
		return
	}
	if err != nil {
		log.Printf("Error getting job: %s\n", err)
		return
	}

	req.Job = *job
	req.AllowStart = make(chan bool)
	canStartJobChan <- req
	shouldStart := <-req.AllowStart
	if !shouldStart {
		return
	}

	<-concurrentJobTokens
	jobRun := JobRun{Job: *job}
	err = job.Run(jobProgressNotifier(), finishedJobChan)
	if err != nil {
		log.Println(err)
		return
	}
	startedJobChan <- jobRun
}
