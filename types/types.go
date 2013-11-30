package types

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

var (
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type JobId uint64

func JobIdFromString(in string) (out JobId, err error) {
	i, err := stringToUint64(in)
	if err != nil {
		return
	}
	out = JobId(i)
	return
}

func (self JobId) String() string {
	return fmt.Sprintf("%d", self)
}

type Job struct {
	Id        JobId  `json:"id"`
	Name      string `json:"name"`
	ScriptSet string `json:"scriptSet"`
}

func randUint64() (out uint64) {
	out = uint64(rng.Uint32())
	out |= uint64(rng.Uint32() << 32)
	return
}

func NewJob() (newJob Job) {
	return Job{Id: JobId(randUint64())}
}

type JobWithRunning struct {
	Job
	Running bool
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

type JobRunId uint64

func (self JobRunId) String() string {
	return fmt.Sprintf("%d", self)
}

func JobRunIdFromString(in string) (out JobRunId, err error) {
	i, err := stringToUint64(in)
	if err != nil {
		return
	}
	out = JobRunId(i)
	return
}

type JobRun struct {
	Id            JobRunId  `json:"id"`
	Job           Job       `json:"-"`
	ScriptDir     string    `json:"-"`
	WorkingDir    string    `json:"-"`
	Status        JobStatus `json:"status"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
	statusChanges chan JobRun
}

func NewJobRun(job Job, scriptDir string, workingDir string, statusChanges chan JobRun) (out JobRun) {
	return JobRun{Id: JobRunId(randUint64()), Job: job, ScriptDir: scriptDir,
		WorkingDir: workingDir, statusChanges: statusChanges}
}

type JobProgress struct {
	JobRun  JobRun
	Time    time.Time
	Line    string
	IsFinal bool
}

type JobRunProgressRequest struct {
	JobRunId     JobRunId
	ProgressChan chan JobProgress
	StopChan     chan bool
}

type JobStatus uint8

const (
	JobUnknown JobStatus = iota
	JobStarted
	JobSucceeded
	JobFailed
)

var jobStatusNames = map[JobStatus]string{
	JobUnknown:   "Unknown",
	JobStarted:   "Started",
	JobSucceeded: "Succeeded",
	JobFailed:    "Failed",
}

func (self JobStatus) String() string {
	return jobStatusNames[self]
}

type Server interface {
	ScriptDir() string

	SubmitJobRunRequest(JobRunRequest)
	RunningJobRunForJob(Job) *JobRun
	ProgressChanForJobRun(JobRun) (progress chan JobProgress, stop chan bool)

	SubJobUpdates() chan interface{}
	SubJobRunUpdates() chan interface{}
	Unsub(chan interface{})
	WaitGroupAdd(n int)
	WaitGroupDone()

	// Store proxies
	AllJobs() ([]Job, error)
	JobById(id JobId) (*Job, error)
	JobByName(name string) (*Job, error)
	WriteJob(Job) error
	JobRunById(JobRunId) (*JobRun, error)
	RunsForJob(Job) ([]JobRun, error)
	LastRunForJob(Job) (*JobRun, error)
	ProgressForJobRun(JobRun) (*[]JobProgress, error)
	DeleteJobRun(JobRun) error
}

type Store interface {
	Init(connectionString string) error
	Close()
	AllJobs() ([]Job, error)
	JobById(id JobId) (*Job, error)
	JobByName(name string) (*Job, error)
	WriteJob(Job) error
	JobRunById(JobRunId) (*JobRun, error)
	RunsForJob(Job) ([]JobRun, error)
	LastRunForJob(Job) (*JobRun, error)
	WriteJobRun(JobRun) error
	ProgressForJobRun(JobRun) (*[]JobProgress, error)
	WriteJobProgress(JobProgress) error
	DeleteJobRun(JobRun) error
}

// ---

func stringToUint64(in string) (uint64, error) {
	return strconv.ParseUint(in, 10, 64)
}
