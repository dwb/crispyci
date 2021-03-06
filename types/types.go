package types

import (
	"crypto/rand"
	"encoding/base64"
	"time"
)

type ProjectId string

func ProjectIdFromString(in string) (out ProjectId, err error) {
	return ProjectId(in), nil
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

func randId() string {
	// 18 bytes * 8 bits is divisible by 3, so no base64 padding
	buf := make([]byte, 18)
	_, err := rand.Read(buf)
	if err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(buf)
}

func NewProject() Project {
	return new(Project).Init()
}

func (self Project) Init() Project {
	self.Id = ProjectId(randId())
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

type ProjectBuildId string

func ProjectBuildIdFromString(in string) (out ProjectBuildId, err error) {
	return ProjectBuildId(in), nil
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
	return ProjectBuild{Id: ProjectBuildId(randId()), Project: project, ScriptDir: scriptDir,
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
