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

type ProjectId uint64

func ProjectIdFromString(in string) (out ProjectId, err error) {
	i, err := stringToUint64(in)
	if err != nil {
		return
	}
	out = ProjectId(i)
	return
}

func (self ProjectId) String() string {
	return fmt.Sprintf("%d", self)
}

type Project struct {
	Id        ProjectId  `json:"id"`
	Name      string `json:"name"`
	ScriptSet string `json:"scriptSet"`
}

func randUint64() (out uint64) {
	out = uint64(rng.Uint32())
	out |= uint64(rng.Uint32() << 32)
	return
}

func NewProject() (newProject Project) {
	return Project{Id: ProjectId(randUint64())}
}

type ProjectWithRunning struct {
	Project
	Running bool
}

type ProjectRunRequest struct {
	ProjectName    string
	Source     string
	ReceivedAt time.Time
	Project        Project
	AllowStart chan bool
}

func NewProjectRunRequest() (req ProjectRunRequest) {
	return ProjectRunRequest{ReceivedAt: time.Now(), AllowStart: make(chan bool)}
}

func (self *ProjectRunRequest) FindProject(store Store) (project *Project, err error) {
	project, err = store.ProjectByName(self.ProjectName)
	return
}

type ProjectRunId uint64

func (self ProjectRunId) String() string {
	return fmt.Sprintf("%d", self)
}

func ProjectRunIdFromString(in string) (out ProjectRunId, err error) {
	i, err := stringToUint64(in)
	if err != nil {
		return
	}
	out = ProjectRunId(i)
	return
}

type ProjectRun struct {
	Id            ProjectRunId  `json:"id"`
	Project           Project       `json:"-"`
	ScriptDir     string    `json:"-"`
	WorkingDir    string    `json:"-"`
	Status        ProjectRunStatus `json:"status"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
	statusChanges chan ProjectRun
	interruptChan chan bool
}

type ProjectRunUpdate struct {
	ProjectRun
	Deleted bool
}

func NewProjectRun(project Project, scriptDir string, workingDir string, statusChanges chan ProjectRun) (out ProjectRun) {
	return ProjectRun{Id: ProjectRunId(randUint64()), Project: project, ScriptDir: scriptDir,
		WorkingDir: workingDir, statusChanges: statusChanges,
		interruptChan: make(chan bool, 1)}
}

type ProjectProgress struct {
	ProjectRun  ProjectRun
	Time    time.Time
	Line    string
	IsFinal bool
}

type ProjectRunProgressRequest struct {
	ProjectRunId     ProjectRunId
	ProgressChan chan ProjectProgress
	StopChan     chan bool
}

type ProjectRunStatus uint8

const (
	ProjectUnknown ProjectRunStatus = iota
	ProjectStarted
	ProjectSucceeded
	ProjectFailed
	ProjectAborted
)

var projectRunStatusNames = map[ProjectRunStatus]string{
	ProjectUnknown:   "Unknown",
	ProjectStarted:   "Started",
	ProjectSucceeded: "Succeeded",
	ProjectFailed:    "Failed",
	ProjectAborted:   "Aborted",
}

func (self ProjectRunStatus) String() string {
	return projectRunStatusNames[self]
}

type Server interface {
	ScriptDir() string

	SubmitProjectRunRequest(ProjectRunRequest)
	RunningProjectRunForProject(Project) *ProjectRun
	ProgressChanForProjectRun(ProjectRun) (progress <-chan ProjectProgress, stop chan<- bool)

	SubProjectUpdates() chan interface{}
	SubProjectRunUpdates() chan interface{}
	Unsub(chan interface{})
	WaitGroupAdd(n int)
	WaitGroupDone()

	// Store proxies
	AllProjects() ([]Project, error)
	ProjectById(id ProjectId) (*Project, error)
	ProjectByName(name string) (*Project, error)
	WriteProject(Project) error
	ProjectRunById(ProjectRunId) (*ProjectRun, error)
	RunsForProject(Project) ([]ProjectRun, error)
	LastRunForProject(Project) (*ProjectRun, error)
	ProgressForProjectRun(ProjectRun) (*[]ProjectProgress, error)
	DeleteProjectRun(ProjectRun) error
}

type Store interface {
	Init(connectionString string) error
	Close()
	AllProjects() ([]Project, error)
	ProjectById(id ProjectId) (*Project, error)
	ProjectByName(name string) (*Project, error)
	WriteProject(Project) error
	ProjectRunById(ProjectRunId) (*ProjectRun, error)
	RunsForProject(Project) ([]ProjectRun, error)
	LastRunForProject(Project) (*ProjectRun, error)
	WriteProjectRun(ProjectRun) error
	ProgressForProjectRun(ProjectRun) (*[]ProjectProgress, error)
	WriteProjectProgress(ProjectProgress) error
	DeleteProjectRun(ProjectRun) error
}

// ---

func stringToUint64(in string) (uint64, error) {
	return strconv.ParseUint(in, 10, 64)
}
