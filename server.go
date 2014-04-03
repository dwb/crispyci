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
const KeepNumOldProjectRuns = 50
const MaxQueuedProjects = 10

const (
	projectsPubSubChannel              = "projects"
	projectRunsPubSubChannel           = "projectRuns"
	projectProgressPubSubChannelPrefix = "projectProgress:"
)

type Server struct {
	scriptDir                 string
	workingDir                string
	store                     types.Store
	concurrentProjectTokens       chan bool
	runProjectChan                chan types.ProjectRunRequest
	canStartProjectChan           chan types.ProjectRunRequest
	projectRunStatusChan             chan types.ProjectRun
	projectEvents                 *pubsub.PubSub
	runningProjects               map[types.ProjectId]*types.ProjectRun
	runningProjectRuns            map[types.ProjectRunId]*types.ProjectRun
	runningProjectsMutex          *sync.Mutex
	waitingProjectRuns            map[types.ProjectId]*list.List
	projectProgressChan           chan types.ProjectProgress
	requestProjectRunProgressChan chan types.ProjectRunProgressRequest
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

	server.concurrentProjectTokens = make(chan bool, MaxConcurrentProjects)
	// Pre-fill with start "tokens"
	for i := 0; i < cap(server.concurrentProjectTokens); i++ {
		server.concurrentProjectTokens <- true
	}
	server.runProjectChan = make(chan types.ProjectRunRequest, 1)
	server.canStartProjectChan = make(chan types.ProjectRunRequest, 5)
	server.projectRunStatusChan = make(chan types.ProjectRun, 5)
	server.projectEvents = pubsub.New(1024)

	server.runningProjects = make(map[types.ProjectId]*types.ProjectRun)
	server.runningProjectRuns = make(map[types.ProjectRunId]*types.ProjectRun)
	server.runningProjectsMutex = new(sync.Mutex)
	server.waitingProjectRuns = make(map[types.ProjectId]*list.List)

	server.projectProgressChan = make(chan types.ProjectProgress)
	server.requestProjectRunProgressChan = make(chan types.ProjectRunProgressRequest)

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
		case req := <-self.runProjectChan:
			self.runProjectFromRequest(req)

		case req := <-self.canStartProjectChan:
			if _, ok := self.runningProjects[req.Project.Id]; ok {
				log.Printf("'%s' is already running; queueing", req.Project.Name)
				id := req.Project.Id
				projectQueue := self.waitingProjectRuns[id]
				if projectQueue == nil {
					projectQueue = list.New()
					self.waitingProjectRuns[id] = projectQueue
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

		case projectRun := <-self.projectRunStatusChan:
			if projectRun.Status == types.ProjectStarted {
				log.Printf("Started project: %s\n", projectRun.Project.Name)
				self.pubProjectRunUpdate(projectRun, false)
				self.runningProjectsMutex.Lock()
				self.runningProjects[projectRun.Project.Id] = &projectRun
				self.runningProjectRuns[projectRun.Id] = &projectRun
				self.runningProjectsMutex.Unlock()
			} else {
				err := self.writeProjectRun(projectRun)
				if err != nil {
					log.Printf("Error writing project run: %s\n", err)
				}

				project := projectRun.Project
				log.Printf("Project finished: %s\n", project.Name)

				self.runningProjectsMutex.Lock()
				delete(self.runningProjects, project.Id)
				delete(self.runningProjectRuns, projectRun.Id)
				self.runningProjectsMutex.Unlock()

				go self.cleanOldRunsForProject(project)

				if self.shouldStop && len(self.runningProjects) == 0 {
					break
				}

				self.concurrentProjectTokens <- true
				if projectQueue, ok := self.waitingProjectRuns[project.Id]; ok {
					reqElement := projectQueue.Back()
					if reqElement != nil {
						req := projectQueue.Remove(reqElement).(types.ProjectRunRequest)
						self.runProjectFromRequest(req)
					}
				}
			}

		case projectProgress := <-self.projectProgressChan:
			self.writeProjectProgress(projectProgress)

		case req := <-self.requestProjectRunProgressChan:
			self.feedProjectProgress(req.ProjectRunId, req.ProgressChan, req.StopChan)

		case <-self.requestStopChan:
			self.shouldStop = true
		}

		if self.shouldStop && len(self.runningProjects) == 0 {
			self.waitGroup.Wait()
			break
		}
	}
}

func (self *Server) Stop() {
	self.requestStopChan <- true
}

func (self *Server) SubmitProjectRunRequest(req types.ProjectRunRequest) {
	self.runProjectChan <- req
}

func (self *Server) WaitGroupAdd(n int) {
	self.waitGroup.Add(n)
}

func (self *Server) WaitGroupDone() {
	self.waitGroup.Done()
}

func (self *Server) RunningProjectRunForProject(project types.Project) (out *types.ProjectRun) {
	self.runningProjectsMutex.Lock()
	out = self.runningProjects[project.Id]
	self.runningProjectsMutex.Unlock()
	return
}

func (self *Server) ProgressChanForProjectRun(projectRun types.ProjectRun) (ch <-chan types.ProjectProgress, stop chan<- bool) {
	ch2way := make(chan types.ProjectProgress, 1)
	stop2way := make(chan bool, 1)
	self.requestProjectRunProgressChan <- types.ProjectRunProgressRequest{
		ProjectRunId: projectRun.Id, ProgressChan: ch2way, StopChan: stop2way}
	return ch2way, stop2way
}

func (self *Server) pubProjectUpdate(project types.Project) {
	self.projectEvents.Pub(project, projectsPubSubChannel)
}

func (self *Server) SubProjectUpdates() (ch chan interface{}) {
	return self.projectEvents.Sub(projectsPubSubChannel)
}

func (self *Server) pubProjectRunUpdate(projectRun types.ProjectRun, deleted bool) {
	self.projectEvents.Pub(types.ProjectRunUpdate{ProjectRun: projectRun, Deleted: deleted},
		projectRunsPubSubChannel)
}

func (self *Server) SubProjectRunUpdates() (ch chan interface{}) {
	return self.projectEvents.Sub(projectRunsPubSubChannel)
}

func (self *Server) pubProjectProgressUpdate(projectProgress types.ProjectProgress) {
	self.projectEvents.Pub(projectProgress,
		projectProgressPubSubChannelPrefix+string(projectProgress.ProjectRun.Id))
}

func (self *Server) SubProjectProgressUpdates(projectRunId types.ProjectRunId) chan interface{} {
	return self.projectEvents.Sub(projectProgressPubSubChannelPrefix + string(projectRunId))
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

func (self *Server) ProjectByName(name string) (*types.Project, error) {
	return self.store.ProjectByName(name)
}

func (self *Server) WriteProject(project types.Project) (err error) {
	err = self.store.WriteProject(project)
	if err == nil {
		self.pubProjectUpdate(project)
	}
	return
}

func (self *Server) ProjectRunById(id types.ProjectRunId) (projectRun *types.ProjectRun, err error) {
	self.runningProjectsMutex.Lock()
	projectRun = self.runningProjectRuns[id]
	self.runningProjectsMutex.Unlock()

	if projectRun == nil {
		projectRun, err = self.store.ProjectRunById(id)
	}
	return
}

func (self *Server) RunsForProject(project types.Project) (out []types.ProjectRun, err error) {
	out, err = self.store.RunsForProject(project)
	if err != nil {
		return
	}
	running := self.RunningProjectRunForProject(project)
	if running != nil {
		out = append(out, *running)
	}
	return
}

func (self *Server) LastRunForProject(project types.Project) (*types.ProjectRun, error) {
	return self.store.LastRunForProject(project)
}

func (self *Server) ProgressForProjectRun(projectRun types.ProjectRun) (*[]types.ProjectProgress, error) {
	return self.store.ProgressForProjectRun(projectRun)
}

func (self *Server) writeProjectRun(projectRun types.ProjectRun) (err error) {
	err = self.store.WriteProjectRun(projectRun)
	if err == nil {
		self.pubProjectRunUpdate(projectRun, false)
	}
	return
}

func (self *Server) DeleteProjectRun(projectRun types.ProjectRun) (err error) {
	err = self.store.DeleteProjectRun(projectRun)
	if err == nil {
		self.pubProjectRunUpdate(projectRun, true)
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

func (self *Server) runProjectFromRequest(req types.ProjectRunRequest) {
	go func() {
		log.Printf("Received project run request for '%s'\n", req.ProjectName)

		project, err := req.FindProject(self.store)
		if project == nil {
			log.Printf("Couldn't find project '%s'\n", req.ProjectName)
			return
		}
		if err != nil {
			log.Printf("Error getting project: %s\n", err)
			return
		}

		req.Project = *project
		self.canStartProjectChan <- req
		shouldStart := <-req.AllowStart
		if !shouldStart {
			return
		}

		<-self.concurrentProjectTokens
		projectRun := types.NewProjectRun(*project, self.scriptDir, self.workingDir,
			self.projectRunStatusChan)
		err = projectRun.Run(self.projectProgressChan)
		if err != nil {
			log.Println(err)
			return
		}
	}()
}

func (self *Server) feedProjectProgress(projectRunId types.ProjectRunId, outChan chan types.ProjectProgress, stopChan chan bool) {
	inChan := self.SubProjectProgressUpdates(projectRunId)
	projectRun, err := self.ProjectRunById(projectRunId)
	if err != nil || projectRun == nil {
		return
	}
	storedProgress, err := self.ProgressForProjectRun(*projectRun)

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

func (self *Server) cleanOldRunsForProject(project types.Project) {
	projectRuns, err := self.store.RunsForProject(project)
	if err != nil {
		log.Printf("Error getting project runs for cleaning: %s\n", err)
		return
	}

	if end := len(projectRuns) - KeepNumOldProjectRuns; end > 0 {
		for _, projectRun := range projectRuns[:end] {
			err := self.DeleteProjectRun(projectRun)
			if err != nil {
				log.Printf("Error deleting old project run: %s\n", err)
			}
		}
	}
}
