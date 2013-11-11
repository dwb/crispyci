package main

import (
	"container/list"
	"fmt"
	"log"
	"net/http"
)

const MaxConcurrentJobs = 5

type Server struct {
	store               Store
	concurrentJobTokens chan bool
	runJobChan          chan JobRunRequest
	canStartJobChan     chan JobRunRequest
	startedJobChan      chan JobRun
	finishedJobChan     chan JobResult
	runningJobs         map[JobId]*JobRun
	waitingJobRuns      map[JobId]*list.List
	httpServer          http.Server
}

func NewServer() (server *Server, err error) {
	server = new(Server)

	// TODO: a useful store!
	server.store = new(StaticStore)
	server.store.Init("")

	server.concurrentJobTokens = make(chan bool, MaxConcurrentJobs)
	// Pre-fill with start "tokens"
	for i := 0; i < cap(server.concurrentJobTokens); i++ {
		server.concurrentJobTokens <- true
	}
	server.runJobChan = make(chan JobRunRequest, 1)
	server.canStartJobChan = make(chan JobRunRequest, 5)
	server.startedJobChan = make(chan JobRun, 5)
	server.finishedJobChan = make(chan JobResult, 5)

	server.runningJobs = make(map[JobId]*JobRun)
	server.waitingJobRuns = make(map[JobId]*list.List)

	server.httpServer = NewHttpInterface(server.runJobChan)
	server.httpServer.Addr = "127.0.0.1:3000"

	return
}

func (self *Server) Serve() {
	go func() {
		log.Printf("Listening on %s ...", self.httpServer.Addr)
		self.httpServer.ListenAndServe()
	}()

	log.Println("Accepting jobs...")
	for {
		select {
		case req := <-self.runJobChan:
			go self.runJobFromRequest(req)

		case req := <-self.canStartJobChan:
			if _, ok := self.runningJobs[req.Job.Id]; ok {
				log.Printf("'%s' is already running; queueing", req.Job.Name)
				id := req.Job.Id
				jobQueue := self.waitingJobRuns[id]
				if jobQueue == nil {
					jobQueue = list.New()
					self.waitingJobRuns[id] = jobQueue
				}
				jobQueue.PushFront(req)
			} else {
				req.AllowStart <- true
			}

		case jobRun := <-self.startedJobChan:
			log.Printf("Started job: %s\n", jobRun.Job.Name)
			self.runningJobs[jobRun.Job.Id] = &jobRun

		case jobResult := <-self.finishedJobChan:
			log.Printf("Job finished: %s\n", jobResult.Job.Name)
			delete(self.runningJobs, jobResult.Job.Id)
			self.concurrentJobTokens <- true
			if jobQueue, ok := self.waitingJobRuns[jobResult.Job.Id]; ok {
				reqElement := jobQueue.Back()
				if reqElement != nil {
					req := jobQueue.Remove(reqElement).(JobRunRequest)
					go self.runJobFromRequest(req)
				}
			}
		}
	}
}

func (self *Server) runJobFromRequest(req JobRunRequest) {
	log.Printf("Received job run request for '%s'\n", req.JobName)

	job, err := req.FindJob(self.store)
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
	self.canStartJobChan <- req
	shouldStart := <-req.AllowStart
	if !shouldStart {
		return
	}

	<-self.concurrentJobTokens
	jobRun := JobRun{Job: *job}
	err = job.Run(self.jobProgressNotifier(), self.finishedJobChan)
	if err != nil {
		log.Println(err)
		return
	}
	self.startedJobChan <- jobRun
}

func (self *Server) jobProgressNotifier() (chanOut chan JobProgress) {
	// TODO: this will need to come out to multiple consumers
	chanOut = make(chan JobProgress, 1)
	go func() {
		for progress := range chanOut {
			fmt.Print(progress.Line)
		}
	}()
	return
}
