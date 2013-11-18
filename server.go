package main

import (
	"container/list"
	"fmt"
	"log"
	"net/http"
	"sync"
)

const MaxConcurrentJobs = 5

type Server struct {
	scriptDir           string
	workingDir          string
	store               Store
	concurrentJobTokens chan bool
	runJobChan          chan JobRunRequest
	canStartJobChan     chan JobRunRequest
	startedJobChan      chan JobRun
	finishedJobChan     chan JobResult
	runningJobs         map[JobId]*JobRun
	waitingJobRuns      map[JobId]*list.List
	waitGroup           *sync.WaitGroup
	requestStopChan     chan bool
	shouldStop          bool
	httpServer          http.Server
}

func NewServer(store Store, scriptDir string, workingDir string, httpInterfaceAddr string) (server *Server, err error) {
	server = new(Server)

	server.scriptDir = scriptDir
	server.workingDir = workingDir

	server.store = store

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

	server.waitGroup = new(sync.WaitGroup)
	server.requestStopChan = make(chan bool, 1)

	server.httpServer = NewHttpInterface(server)
	server.httpServer.Addr = httpInterfaceAddr

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
			self.runJobFromRequest(req)

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
			if self.shouldStop && len(self.runningJobs) == 0 {
				break
			}
			self.concurrentJobTokens <- true
			if jobQueue, ok := self.waitingJobRuns[jobResult.Job.Id]; ok {
				reqElement := jobQueue.Back()
				if reqElement != nil {
					req := jobQueue.Remove(reqElement).(JobRunRequest)
					self.runJobFromRequest(req)
				}
			}

		case <-self.requestStopChan:
			self.shouldStop = true
		}

		if self.shouldStop && len(self.runningJobs) == 0 {
			self.waitGroup.Wait()
			break
		}
	}
}

func (self *Server) Stop() {
	self.requestStopChan <- true
}

func (self *Server) SubmitJobRunRequest(req JobRunRequest) {
	self.runJobChan <- req
}

func (self *Server) WaitGroupAdd(n int) {
	self.waitGroup.Add(n)
}

func (self *Server) WaitGroupDone() {
	self.waitGroup.Done()
}

// ---

func (self *Server) runJobFromRequest(req JobRunRequest) {
	go func() {
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
		self.canStartJobChan <- req
		shouldStart := <-req.AllowStart
		if !shouldStart {
			return
		}

		<-self.concurrentJobTokens
		jobRun := JobRun{Job: *job, ScriptDir: self.scriptDir, WorkingDir: self.workingDir}
		err = jobRun.Run(self.jobProgressNotifier(), self.finishedJobChan)
		if err != nil {
			log.Println(err)
			return
		}
		self.startedJobChan <- jobRun
	}()
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
