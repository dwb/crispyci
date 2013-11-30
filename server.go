package main

import (
	"container/list"
	"github.com/tuxychandru/pubsub"
	"log"
	"net/http"
	"sync"

	"github.com/dwb/crispyci/types"
	"github.com/dwb/crispyci/webui"
)

const MaxConcurrentJobs = 5

const (
	jobsPubSubChannel              = "jobs"
	jobRunsPubSubChannel           = "jobRuns"
	jobProgressPubSubChannelPrefix = "jobProgress:"
)

type Server struct {
	scriptDir                 string
	workingDir                string
	store                     types.Store
	concurrentJobTokens       chan bool
	runJobChan                chan types.JobRunRequest
	canStartJobChan           chan types.JobRunRequest
	jobStatusChan             chan types.JobRun
	jobEvents                 *pubsub.PubSub
	runningJobs               map[types.JobId]*types.JobRun
	runningJobRuns            map[types.JobRunId]*types.JobRun
	runningJobsMutex          *sync.Mutex
	waitingJobRuns            map[types.JobId]*list.List
	jobProgressChan           chan types.JobProgress
	requestJobRunProgressChan chan types.JobRunProgressRequest
	waitGroup                 *sync.WaitGroup
	requestStopChan           chan bool
	shouldStop                bool
	httpServer                http.Server
}

func NewServer(store types.Store, scriptDir string, workingDir string, httpInterfaceAddr string) (server *Server, err error) {
	server = new(Server)

	server.scriptDir = scriptDir
	server.workingDir = workingDir

	server.store = store

	server.concurrentJobTokens = make(chan bool, MaxConcurrentJobs)
	// Pre-fill with start "tokens"
	for i := 0; i < cap(server.concurrentJobTokens); i++ {
		server.concurrentJobTokens <- true
	}
	server.runJobChan = make(chan types.JobRunRequest, 1)
	server.canStartJobChan = make(chan types.JobRunRequest, 5)
	server.jobStatusChan = make(chan types.JobRun, 5)
	server.jobEvents = pubsub.New(1024)

	server.runningJobs = make(map[types.JobId]*types.JobRun)
	server.runningJobRuns = make(map[types.JobRunId]*types.JobRun)
	server.runningJobsMutex = new(sync.Mutex)
	server.waitingJobRuns = make(map[types.JobId]*list.List)

	server.jobProgressChan = make(chan types.JobProgress)
	server.requestJobRunProgressChan = make(chan types.JobRunProgressRequest)

	server.waitGroup = new(sync.WaitGroup)
	server.requestStopChan = make(chan bool, 1)

	server.httpServer = webui.New(server)
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

		case jobRun := <-self.jobStatusChan:
			self.pubJobRunUpdate(jobRun)
			if jobRun.Status == types.JobStarted {
				log.Printf("Started job: %s\n", jobRun.Job.Name)
				self.runningJobsMutex.Lock()
				self.runningJobs[jobRun.Job.Id] = &jobRun
				self.runningJobRuns[jobRun.Id] = &jobRun
				self.runningJobsMutex.Unlock()
			} else {
				err := self.writeJobRun(jobRun)
				if err != nil {
					log.Printf("Error writing job run: %s\n", err)
				}

				job := jobRun.Job
				log.Printf("Job finished: %s\n", job.Name)

				self.runningJobsMutex.Lock()
				delete(self.runningJobs, job.Id)
				delete(self.runningJobRuns, jobRun.Id)
				self.runningJobsMutex.Unlock()

				if self.shouldStop && len(self.runningJobs) == 0 {
					break
				}

				self.concurrentJobTokens <- true
				if jobQueue, ok := self.waitingJobRuns[job.Id]; ok {
					reqElement := jobQueue.Back()
					if reqElement != nil {
						req := jobQueue.Remove(reqElement).(types.JobRunRequest)
						self.runJobFromRequest(req)
					}
				}
			}

		case jobProgress := <-self.jobProgressChan:
			self.writeJobProgress(jobProgress)

		case req := <-self.requestJobRunProgressChan:
			self.feedJobProgress(req.JobRunId, req.ProgressChan, req.StopChan)

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

func (self *Server) SubmitJobRunRequest(req types.JobRunRequest) {
	self.runJobChan <- req
}

func (self *Server) WaitGroupAdd(n int) {
	self.waitGroup.Add(n)
}

func (self *Server) WaitGroupDone() {
	self.waitGroup.Done()
}

func (self *Server) RunningJobRunForJob(job types.Job) (out *types.JobRun) {
	self.runningJobsMutex.Lock()
	out = self.runningJobs[job.Id]
	self.runningJobsMutex.Unlock()
	return
}

func (self *Server) ProgressChanForJobRun(jobRun types.JobRun) (ch chan types.JobProgress, stop chan bool) {
	ch = make(chan types.JobProgress, 1)
	stop = make(chan bool, 1)
	self.requestJobRunProgressChan <- types.JobRunProgressRequest{
		JobRunId: jobRun.Id, ProgressChan: ch, StopChan: stop}
	return
}

func (self *Server) pubJobUpdate(job types.Job) {
	self.jobEvents.Pub(job, jobsPubSubChannel)
}

func (self *Server) SubJobUpdates() (ch chan interface{}) {
	return self.jobEvents.Sub(jobsPubSubChannel)
}

func (self *Server) pubJobRunUpdate(jobRun types.JobRun) {
	self.jobEvents.Pub(jobRun, jobRunsPubSubChannel)
}

func (self *Server) SubJobRunUpdates() (ch chan interface{}) {
	return self.jobEvents.Sub(jobRunsPubSubChannel)
}

func (self *Server) pubJobProgressUpdate(jobProgress types.JobProgress) {
	self.jobEvents.Pub(jobProgress,
		jobProgressPubSubChannelPrefix+string(jobProgress.JobRun.Id))
}

func (self *Server) SubJobProgressUpdates(jobRunId types.JobRunId) chan interface{} {
	return self.jobEvents.Sub(jobProgressPubSubChannelPrefix + string(jobRunId))
}

func (self *Server) Unsub(ch chan interface{}) {
	self.jobEvents.Unsub(ch)
}

func (self *Server) ScriptDir() string {
	return self.scriptDir
}

// --- Store proxies ---

func (self *Server) AllJobs() ([]types.Job, error) {
	return self.store.AllJobs()
}

func (self *Server) JobById(id types.JobId) (*types.Job, error) {
	return self.store.JobById(id)
}

func (self *Server) JobByName(name string) (*types.Job, error) {
	return self.store.JobByName(name)
}

func (self *Server) WriteJob(job types.Job) (err error) {
	err = self.store.WriteJob(job)
	if err == nil {
		self.pubJobUpdate(job)
	}
	return
}

func (self *Server) JobRunById(id types.JobRunId) (jobRun *types.JobRun, err error) {
	self.runningJobsMutex.Lock()
	jobRun = self.runningJobRuns[id]
	self.runningJobsMutex.Unlock()

	if jobRun == nil {
		jobRun, err = self.store.JobRunById(id)
	}
	return
}

func (self *Server) RunsForJob(job types.Job) (out []types.JobRun, err error) {
	out, err = self.store.RunsForJob(job)
	if err != nil {
		return
	}
	running := self.RunningJobRunForJob(job)
	if running != nil {
		out = append(out, *running)
	}
	return
}

func (self *Server) LastRunForJob(job types.Job) (*types.JobRun, error) {
	return self.store.LastRunForJob(job)
}

func (self *Server) ProgressForJobRun(jobRun types.JobRun) (*[]types.JobProgress, error) {
	return self.store.ProgressForJobRun(jobRun)
}

func (self *Server) writeJobRun(jobRun types.JobRun) (err error) {
	err = self.store.WriteJobRun(jobRun)
	if err == nil {
		self.pubJobRunUpdate(jobRun)
	}
	return
}

func (self *Server) DeleteJobRun(jobRun types.JobRun) error {
	return self.store.DeleteJobRun(jobRun)
}

func (self *Server) writeJobProgress(p types.JobProgress) (err error) {
	err = self.store.WriteJobProgress(p)
	if err == nil {
		self.pubJobProgressUpdate(p)
	}
	return
}

// --- Private ---

func (self *Server) runJobFromRequest(req types.JobRunRequest) {
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
		jobRun := types.NewJobRun(*job, self.scriptDir, self.workingDir,
			self.jobStatusChan)
		err = jobRun.Run(self.jobProgressChan)
		if err != nil {
			log.Println(err)
			return
		}
	}()
}

func (self *Server) feedJobProgress(jobRunId types.JobRunId, outChan chan types.JobProgress, stopChan chan bool) {
	inChan := self.SubJobProgressUpdates(jobRunId)
	jobRun, err := self.JobRunById(jobRunId)
	if err != nil || jobRun == nil {
		return
	}
	storedProgress, err := self.ProgressForJobRun(*jobRun)

	go func() {
		defer self.Unsub(inChan)
		defer close(outChan)

		var p types.JobProgress
		for _, p = range *storedProgress {
			outChan <- p
			select {
			case _ = <-stopChan:
				return
			default:
			}
		}
		if p.IsFinal {
			return
		}
		lastTime := p.Time

		for {
			select {
			case pI := <-inChan:
				if p, ok := pI.(types.JobProgress); ok && p.Time.After(lastTime) {
					outChan <- p
					if p.IsFinal {
						return
					}
				}
			case _ = <-stopChan:
				return
			}
		}
	}()
}
