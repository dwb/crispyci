package main

import (
	"math/rand"
	"time"
)

var (
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type JobId uint64

type Job struct {
	Id        JobId
	Name      string
	ScriptSet string
}

func newJobId() (id JobId) {
	id = JobId(rng.Uint32())
	id |= JobId(rng.Uint32() << 32)
	return
}

func NewJob() (newJob Job) {
	return Job{Id: newJobId()}
}

type JobRunRequest struct {
	JobName    string
	Source     string
	ReceivedAt time.Time
	Job        Job
	AllowStart chan bool
}

func NewJobRunRequest() (req JobRunRequest) {
	return JobRunRequest{ReceivedAt: time.Now(), AllowStart: make(chan bool)}
}

func (self *JobRunRequest) FindJob(store Store) (job *Job, err error) {
	job, err = store.JobByName(self.JobName)
	return
}

type JobRun struct {
	Job        Job
	ScriptDir  string
	WorkingDir string
}

type JobProgress struct {
	Job  Job
	Line string
}

type JobStatus int

const (
	JobSucceeded JobStatus = iota
	JobFailed
	JobUnknown
)

type JobResult struct {
	Job        Job
	Status     JobStatus
	StartedAt  time.Time
	FinishedAt time.Time
	Output     string
}

type Store interface {
	Init(connectionString string) error
	Close()
	AllJobs() ([]Job, error)
	JobByName(name string) (*Job, error)
	WriteJob(*Job) error
	ResultsForJob(*Job) ([]JobResult, error)
	WriteJobResult(JobResult) error
}
