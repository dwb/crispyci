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
	Id        ProjectId `json:"id"`
	Name      string    `json:"name"`
	Url       string    `json:"url"`
	ScriptSet string    `json:"scriptSet"`
}

type ProjectUpdate struct {
	Project
	Deleted bool
}

func randUint64() (out uint64) {
	out = uint64(rng.Uint32())
	out |= uint64(rng.Uint32()) << 32
	return
}

func NewProject() Project {
	return new(Project).Init()
}

func (self Project) Init() Project {
	self.Id = ProjectId(randUint64())
	return self
}

type ProjectWithBuilding struct {
	Project
	Building bool
}

type ProjectBuildRequest struct {
	Url        string
	Branch     string
	FromCommit string
	ToCommit   string
	Source     string
	ReceivedAt time.Time
	AllowStart chan bool
	Project    Project
}

func NewProjectBuildRequest() (req ProjectBuildRequest) {
	return ProjectBuildRequest{ReceivedAt: time.Now(), AllowStart: make(chan bool)}
}

func (self *ProjectBuildRequest) FindProject(store Store) (err error) {
	project, err := store.ProjectByUrl(self.Url)
	if err == nil {
		self.Project = *project
	}
	return
}

type ProjectBuildId uint64

func (self ProjectBuildId) String() string {
	return fmt.Sprintf("%d", self)
}

func ProjectBuildIdFromString(in string) (out ProjectBuildId, err error) {
	i, err := stringToUint64(in)
	if err != nil {
		return
	}
	out = ProjectBuildId(i)
	return
}

type ProjectBuild struct {
	Id            ProjectBuildId     `json:"id"`
	Project       Project            `json:"-"`
	ScriptDir     string             `json:"-"`
	WorkingDir    string             `json:"-"`
	Status        ProjectBuildStatus `json:"status"`
	StartedAt     time.Time          `json:"startedAt"`
	FinishedAt    time.Time          `json:"finishedAt"`
	statusChanges chan ProjectBuild
	interruptChan chan bool
}

type ProjectBuildUpdate struct {
	ProjectBuild
	Deleted bool
}

func NewProjectBuild(project Project, scriptDir string, workingDir string, statusChanges chan ProjectBuild) (out ProjectBuild) {
	return ProjectBuild{Id: ProjectBuildId(randUint64()), Project: project, ScriptDir: scriptDir,
		WorkingDir: workingDir, statusChanges: statusChanges,
		interruptChan: make(chan bool, 1)}
}

type ProjectProgress struct {
	ProjectBuild ProjectBuild
	Time         time.Time
	Line         string
	IsFinal      bool
}

type ProjectBuildProgressRequest struct {
	ProjectBuildId ProjectBuildId
	ProgressChan   chan ProjectProgress
	StopChan       chan bool
}

type ProjectBuildStatus uint8

const (
	ProjectUnknown ProjectBuildStatus = iota
	ProjectStarted
	ProjectSucceeded
	ProjectFailed
	ProjectAborted
)

var projectBuildStatusNames = map[ProjectBuildStatus]string{
	ProjectUnknown:   "Unknown",
	ProjectStarted:   "Started",
	ProjectSucceeded: "Succeeded",
	ProjectFailed:    "Failed",
	ProjectAborted:   "Aborted",
}

func (self ProjectBuildStatus) String() string {
	return projectBuildStatusNames[self]
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Server interface {
	ScriptDir() string

	SubmitProjectBuildRequest(ProjectBuildRequest)
	BuildingProjectBuildForProject(Project) *ProjectBuild
	ProgressChanForProjectBuild(ProjectBuild) (progress <-chan ProjectProgress, stop chan<- bool)

	SubProjectUpdates() chan interface{}
	SubProjectBuildUpdates() chan interface{}
	Unsub(chan interface{})
	WaitGroupAdd(n int)
	WaitGroupDone()

	// Store proxies
	AllProjects() ([]Project, error)
	ProjectById(id ProjectId) (*Project, error)
	ProjectByUrl(url string) (*Project, error)
	WriteProject(Project) error
	DeleteProject(Project) error
	ProjectBuildById(ProjectBuildId) (*ProjectBuild, error)
	BuildsForProject(Project) ([]ProjectBuild, error)
	LastBuildForProject(Project) (*ProjectBuild, error)
	ProgressForProjectBuild(ProjectBuild) (*[]ProjectProgress, error)
	DeleteProjectBuild(ProjectBuild) error
}

type Store interface {
	Init(connectionString string) error
	Close()
	AllProjects() ([]Project, error)
	ProjectById(id ProjectId) (*Project, error)
	ProjectByUrl(url string) (*Project, error)
	WriteProject(Project) error
	DeleteProject(Project) error
	ProjectBuildById(ProjectBuildId) (*ProjectBuild, error)
	BuildsForProject(Project) ([]ProjectBuild, error)
	LastBuildForProject(Project) (*ProjectBuild, error)
	WriteProjectBuild(ProjectBuild) error
	ProgressForProjectBuild(ProjectBuild) (*[]ProjectProgress, error)
	WriteProjectProgress(ProjectProgress) error
	DeleteProjectBuild(ProjectBuild) error
}

// ---

func stringToUint64(in string) (uint64, error) {
	return strconv.ParseUint(in, 10, 64)
}
