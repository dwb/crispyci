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

const MaxConcurrentProjects = 5
const KeepNumOldProjectBuilds = 50
const MaxQueuedProjects = 10

const (
	projectsPubSubChannel              = "projects"
	projectBuildsPubSubChannel         = "projectBuilds"
	projectProgressPubSubChannelPrefix = "projectProgress:"
)

type Server struct {
	scriptDir                       string
	workingDir                      string
	store                           types.Store
	concurrentProjectTokens         chan bool
	buildProjectChan                chan types.ProjectBuildRequest
	canStartProjectChan             chan types.ProjectBuildRequest
	projectBuildStatusChan          chan types.ProjectBuild
	projectEvents                   *pubsub.PubSub
	buildingProjects                map[types.ProjectId]*types.ProjectBuild
	buildingProjectBuilds           map[types.ProjectBuildId]*types.ProjectBuild
	buildingProjectsMutex           *sync.Mutex
	waitingProjectBuilds            map[types.ProjectId]*list.List
	projectProgressChan             chan types.ProjectProgress
	requestProjectBuildProgressChan chan types.ProjectBuildProgressRequest
	waitGroup                       *sync.WaitGroup
	requestStopChan                 chan bool
	shouldStop                      bool
	httpServer                      http.Server
}

func NewServer(store types.Store, scriptDir string, workingDir string, httpInterfaceAddr string) (server *Server, err error) {
	server = new(Server)

	server.scriptDir = scriptDir
	server.workingDir = workingDir

	server.store = store

	server.concurrentProjectTokens = make(chan bool, MaxConcurrentProjects)
	// Pre-fill with start "tokens"
	for i := 0; i < cap(server.concurrentProjectTokens); i++ {
		server.concurrentProjectTokens <- true
	}
	server.buildProjectChan = make(chan types.ProjectBuildRequest, 1)
	server.canStartProjectChan = make(chan types.ProjectBuildRequest, 5)
	server.projectBuildStatusChan = make(chan types.ProjectBuild, 5)
	server.projectEvents = pubsub.New(1024)

	server.buildingProjects = make(map[types.ProjectId]*types.ProjectBuild)
	server.buildingProjectBuilds = make(map[types.ProjectBuildId]*types.ProjectBuild)
	server.buildingProjectsMutex = new(sync.Mutex)
	server.waitingProjectBuilds = make(map[types.ProjectId]*list.List)

	server.projectProgressChan = make(chan types.ProjectProgress)
	server.requestProjectBuildProgressChan = make(chan types.ProjectBuildProgressRequest)

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

	log.Println("Accepting projects...")
	for {
		select {
		case req := <-self.buildProjectChan:
			self.buildProjectFromRequest(req)

		case req := <-self.canStartProjectChan:
			if _, ok := self.buildingProjects[req.Project.Id]; ok {
				log.Printf("'%s' is already building; queueing", req.Project.Name)
				req.AllowStart <- false
				id := req.Project.Id
				projectQueue := self.waitingProjectBuilds[id]
				if projectQueue == nil {
					projectQueue = list.New()
					self.waitingProjectBuilds[id] = projectQueue
				}
				projectQueue.PushFront(req)
				for lenOver := projectQueue.Len() - MaxQueuedProjects; lenOver > 0; lenOver-- {
					e := projectQueue.Back()
					if e != nil {
						projectQueue.Remove(e)
					}
				}
			} else {
				req.AllowStart <- true
			}

		case projectBuild := <-self.projectBuildStatusChan:
			if projectBuild.Status == types.ProjectStarted {
				log.Printf("Started project: %s\n", projectBuild.Project.Name)
				self.pubProjectBuildUpdate(projectBuild, false)
				self.buildingProjectsMutex.Lock()
				self.buildingProjects[projectBuild.Project.Id] = &projectBuild
				self.buildingProjectBuilds[projectBuild.Id] = &projectBuild
				self.buildingProjectsMutex.Unlock()
			} else {
				err := self.writeProjectBuild(projectBuild)
				if err != nil {
					log.Printf("Error writing project build: %s\n", err)
				}

				project := projectBuild.Project
				log.Printf("Project finished: %s\n", project.Name)

				self.buildingProjectsMutex.Lock()
				delete(self.buildingProjects, project.Id)
				delete(self.buildingProjectBuilds, projectBuild.Id)
				self.buildingProjectsMutex.Unlock()

				go self.cleanOldBuildsForProject(project)

				if self.shouldStop && len(self.buildingProjects) == 0 {
					break
				}

				self.concurrentProjectTokens <- true
				if projectQueue, ok := self.waitingProjectBuilds[project.Id]; ok {
					reqElement := projectQueue.Back()
					if reqElement != nil {
						req := projectQueue.Remove(reqElement).(types.ProjectBuildRequest)
						self.buildProjectFromRequest(req)
					}
				}
			}

		case projectProgress := <-self.projectProgressChan:
			self.writeProjectProgress(projectProgress)

		case req := <-self.requestProjectBuildProgressChan:
			self.feedProjectProgress(req.ProjectBuildId, req.ProgressChan, req.StopChan)

		case <-self.requestStopChan:
			self.shouldStop = true
		}

		if self.shouldStop && len(self.buildingProjects) == 0 {
			self.waitGroup.Wait()
			break
		}
	}
}

func (self *Server) Stop() {
	self.requestStopChan <- true
}

func (self *Server) SubmitProjectBuildRequest(req types.ProjectBuildRequest) {
	self.buildProjectChan <- req
}

func (self *Server) WaitGroupAdd(n int) {
	self.waitGroup.Add(n)
}

func (self *Server) WaitGroupDone() {
	self.waitGroup.Done()
}

func (self *Server) BuildingProjectBuildForProject(project types.Project) (out *types.ProjectBuild) {
	self.buildingProjectsMutex.Lock()
	out = self.buildingProjects[project.Id]
	self.buildingProjectsMutex.Unlock()
	return
}

func (self *Server) ProgressChanForProjectBuild(projectBuild types.ProjectBuild) (ch <-chan types.ProjectProgress, stop chan<- bool) {
	ch2way := make(chan types.ProjectProgress, 1)
	stop2way := make(chan bool, 1)
	self.requestProjectBuildProgressChan <- types.ProjectBuildProgressRequest{
		ProjectBuildId: projectBuild.Id, ProgressChan: ch2way, StopChan: stop2way}
	return ch2way, stop2way
}

func (self *Server) pubProjectUpdate(project types.Project, deleted bool) {
	self.projectEvents.Pub(
		types.ProjectUpdate{Project: project, Deleted: deleted},
		projectsPubSubChannel)
}

func (self *Server) SubProjectUpdates() (ch chan interface{}) {
	return self.projectEvents.Sub(projectsPubSubChannel)
}

func (self *Server) pubProjectBuildUpdate(projectBuild types.ProjectBuild, deleted bool) {
	self.projectEvents.Pub(types.ProjectBuildUpdate{ProjectBuild: projectBuild, Deleted: deleted},
		projectBuildsPubSubChannel)
}

func (self *Server) SubProjectBuildUpdates() (ch chan interface{}) {
	return self.projectEvents.Sub(projectBuildsPubSubChannel)
}

func (self *Server) pubProjectProgressUpdate(projectProgress types.ProjectProgress) {
	self.projectEvents.Pub(projectProgress,
		projectProgressPubSubChannelPrefix+string(projectProgress.ProjectBuild.Id))
}

func (self *Server) SubProjectProgressUpdates(projectBuildId types.ProjectBuildId) chan interface{} {
	return self.projectEvents.Sub(projectProgressPubSubChannelPrefix + string(projectBuildId))
}

func (self *Server) Unsub(ch chan interface{}) {
	self.projectEvents.Unsub(ch)
}

func (self *Server) ScriptDir() string {
	return self.scriptDir
}

// --- Store proxies ---

func (self *Server) AllProjects() ([]types.Project, error) {
	return self.store.AllProjects()
}

func (self *Server) ProjectById(id types.ProjectId) (*types.Project, error) {
	return self.store.ProjectById(id)
}

func (self *Server) ProjectByUrl(url string) (*types.Project, error) {
	return self.store.ProjectByUrl(url)
}

func (self *Server) WriteProject(project types.Project) (err error) {
	err = self.store.WriteProject(project)
	if err == nil {
		self.pubProjectUpdate(project, false)
	}
	return
}

func (self *Server) DeleteProject(project types.Project) (err error) {
	err = self.store.DeleteProject(project)
	if err == nil {
		self.pubProjectUpdate(project, true)
	}
	return
}

func (self *Server) ProjectBuildById(id types.ProjectBuildId) (projectBuild *types.ProjectBuild, err error) {
	self.buildingProjectsMutex.Lock()
	projectBuild = self.buildingProjectBuilds[id]
	self.buildingProjectsMutex.Unlock()

	if projectBuild == nil {
		projectBuild, err = self.store.ProjectBuildById(id)
	}
	return
}

func (self *Server) BuildsForProject(project types.Project) (out []types.ProjectBuild, err error) {
	out, err = self.store.BuildsForProject(project)
	if err != nil {
		return
	}
	building := self.BuildingProjectBuildForProject(project)
	if building != nil {
		out = append(out, *building)
	}
	return
}

func (self *Server) LastBuildForProject(project types.Project) (*types.ProjectBuild, error) {
	return self.store.LastBuildForProject(project)
}

func (self *Server) ProgressForProjectBuild(projectBuild types.ProjectBuild) (*[]types.ProjectProgress, error) {
	return self.store.ProgressForProjectBuild(projectBuild)
}

func (self *Server) writeProjectBuild(projectBuild types.ProjectBuild) (err error) {
	err = self.store.WriteProjectBuild(projectBuild)
	if err == nil {
		self.pubProjectBuildUpdate(projectBuild, false)
	}
	return
}

func (self *Server) DeleteProjectBuild(projectBuild types.ProjectBuild) (err error) {
	err = self.store.DeleteProjectBuild(projectBuild)
	if err == nil {
		self.pubProjectBuildUpdate(projectBuild, true)
	}
	return
}

func (self *Server) writeProjectProgress(p types.ProjectProgress) (err error) {
	err = self.store.WriteProjectProgress(p)
	if err == nil {
		self.pubProjectProgressUpdate(p)
	}
	return
}

// --- Private ---

func (self *Server) buildProjectFromRequest(req types.ProjectBuildRequest) {
	go func() {
		log.Printf("Received project build request for '%s'\n", req.Url)

		err := req.FindProject(self.store)
		if err != nil {
			log.Printf("Error getting project: %s\n", err)
			return
		}

		self.canStartProjectChan <- req
		shouldStart := <-req.AllowStart
		if !shouldStart {
			return
		}

		<-self.concurrentProjectTokens
		projectBuild := types.NewProjectBuild(req.Project, self.scriptDir,
			self.workingDir, self.projectBuildStatusChan)
		err = projectBuild.Build(self.projectProgressChan)
		if err != nil {
			log.Println(err)
			return
		}
	}()
}

func (self *Server) feedProjectProgress(projectBuildId types.ProjectBuildId, outChan chan types.ProjectProgress, stopChan chan bool) {
	inChan := self.SubProjectProgressUpdates(projectBuildId)
	projectBuild, err := self.ProjectBuildById(projectBuildId)
	if err != nil || projectBuild == nil {
		return
	}
	storedProgress, err := self.ProgressForProjectBuild(*projectBuild)

	go func() {
		defer self.Unsub(inChan)
		defer close(outChan)

		var p types.ProjectProgress
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
				if p, ok := pI.(types.ProjectProgress); ok && p.Time.After(lastTime) {
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

func (self *Server) cleanOldBuildsForProject(project types.Project) {
	projectBuilds, err := self.store.BuildsForProject(project)
	if err != nil {
		log.Printf("Error getting project builds for cleaning: %s\n", err)
		return
	}

	if end := len(projectBuilds) - KeepNumOldProjectBuilds; end > 0 {
		for _, projectBuild := range projectBuilds[:end] {
			err := self.DeleteProjectBuild(projectBuild)
			if err != nil {
				log.Printf("Error deleting old project build: %s\n", err)
			}
		}
	}
}
