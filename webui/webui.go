package webui

import (
	"bytes"
	"code.google.com/p/go.net/websocket"
	"encoding/json"
	"github.com/gorilla/mux"
	"net/http"
	"path"
	"regexp"

	"github.com/dwb/crispyci/types"
)

type githubWebhookPayload struct {
	Repository struct {
		Name string
	}
	Pusher struct {
		Email string
		Name  string
	}
	Ref string
}

type projectsIndexResponse struct {
	Projects []projectWithBuilds `json:"projects"`
}

type projectShowResponse struct {
	Project projectWithBuilds `json:"project"`
}

type projectWithBuilds struct {
	types.Project
	ProjectBuilds []projectBuildWithProjectId `json:"projectBuilds"`
}

type projectBuildWithProjectId struct {
	types.ProjectBuild
	Project types.ProjectId `json:"project"`
}

type projectBuildShowResponse struct {
	ProjectBuild projectBuildWithProjectId `json:"projectBuild"`
}

type projectBuildUpdateResponse struct {
	ProjectBuild projectBuildWithProjectId `json:"projectBuild"`
	Deleted      bool                      `json:"deleted"`
}

const StatusUnprocessableEntity = 422

var branchRefPattern = regexp.MustCompile(`^refs/heads/(\w+)$`)

func New(server types.Server) (out http.Server) {
	r := mux.NewRouter()
	rApi := r.PathPrefix("/api/v1/").Subrouter()

	handleWebsocket := func(w http.ResponseWriter, request *http.Request, f func(http.ResponseWriter, *http.Request, *websocket.Conn, http.CloseNotifier)) {
		wcn, ok := w.(http.CloseNotifier)
		if !ok {
			http.Error(w, "No http.CloseNotifier support",
				http.StatusInternalServerError)
			return
		}

		// Don't hold-up shut-down
		server.WaitGroupDone()

		websocket.Handler(func(ws *websocket.Conn) {
			f(w, request, ws, wcn)
		}).ServeHTTP(w, request)

		server.WaitGroupAdd(1)
	}

	rApi.
		Methods("GET").
		Path("/projects").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projects, err := server.AllProjects()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		projectsWithBuilds := make([]projectWithBuilds, 0, len(projects))
		for _, project := range projects {
			projectBuilds, err := getProjectWithBuilds(&project, server)
			if err != nil {
				continue
			}
			projectsWithBuilds = append(projectsWithBuilds, projectBuilds)
		}

		writeHttpJSON(w, projectsIndexResponse{Projects: projectsWithBuilds})
	})

	rApi.
		Methods("GET").
		Path("/projects/{id}").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projectId, err := types.ProjectIdFromString(mux.Vars(request)["id"])
		if err != nil || projectId <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		project, err := server.ProjectById(types.ProjectId(projectId))
		if err != nil || project == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		jr, err := getProjectWithBuilds(project, server)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		writeHttpJSON(w, projectShowResponse{Project: jr})
	})

	rApi.
		Methods("POST").
		Path("/projects").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		dec := json.NewDecoder(request.Body)
		newProject := types.NewProject()
		err := dec.Decode(&newProject)
		if err != nil {
			w.WriteHeader(StatusUnprocessableEntity)
			w.Write([]byte("JSON parse error\n"))
			return
		}

		// TODO: input validation

		err = server.WriteProject(newProject)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			// TODO: better error message / logging
			w.Write([]byte("Save error\n"))
			return
		}

		// TODO: location header for new project
		w.WriteHeader(http.StatusCreated)
	})

	rApi.
		Path("/projectBuilds/updates").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		handleWebsocket(w, request, func(w http.ResponseWriter, request *http.Request, ws *websocket.Conn, wcn http.CloseNotifier) {

			projectBuilds := server.SubProjectBuildUpdates()
			defer server.Unsub(projectBuilds)

			for {
				select {
				case projectBuildI := <-projectBuilds:
					projectBuildUpdate, ok := projectBuildI.(types.ProjectBuildUpdate)
					if !ok {
						continue
					}

					resp := projectBuildUpdateResponse{
						ProjectBuild: projectBuildWithProjectId{ProjectBuild: projectBuildUpdate.ProjectBuild,
							Project: projectBuildUpdate.Project.Id},
						Deleted: projectBuildUpdate.Deleted}
					websocket.JSON.Send(ws, resp)
				case _ = <-wcn.CloseNotify():
					return
				}
			}
		})
	})

	rApi.
		Path("/projectBuilds/{id}").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projectBuild := getProjectBuild(w, request, server)
		if projectBuild == nil {
			return
		}

		writeHttpJSON(w, projectBuildShowResponse{
			ProjectBuild: projectBuildWithProjectId{ProjectBuild: *projectBuild, Project: projectBuild.Project.Id}})
	})

	rApi.
		Path("/projectBuilds/{id}/progress").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projectBuild := getProjectBuild(w, request, server)
		if projectBuild == nil {
			return
		}

		handleWebsocket(w, request, func(w http.ResponseWriter,
			request *http.Request, ws *websocket.Conn, wcn http.CloseNotifier) {

			progressChan, stopProgress := server.ProgressChanForProjectBuild(*projectBuild)
			defer func() {
				stopProgress <- true
			}()

			for {
				select {
				case progress, isOpen := <-progressChan:
					if !isOpen {
						return
					}
					out := []byte(progress.Line)
					_, err := ws.Write(out)
					if err != nil {
						return
					}

				case _ = <-wcn.CloseNotify():
					return
				}
			}

		})
	})

	rApi.
		Methods("POST").
		Path("/projectBuilds/{id}/abort").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projectBuild := getProjectBuild(w, request, server)
		if projectBuild == nil {
			return
		}

		projectBuild.Abort()
		w.WriteHeader(http.StatusNoContent)
	})

	rApi.
		Methods("POST").
		Path("/githubPostReceive").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		var payload githubWebhookPayload
		err := json.Unmarshal([]byte(request.FormValue("payload")), &payload)
		if err != nil {
			w.WriteHeader(StatusUnprocessableEntity)
			return
		}

		name := payload.Repository.Name
		m := branchRefPattern.FindStringSubmatch(payload.Ref)
		if m != nil {
			name += "-" + m[1]
		}

		req := types.NewProjectBuildRequest()
		req.ProjectName = name
		req.Source = "Github"
		server.SubmitProjectBuildRequest(req)
		w.WriteHeader(http.StatusNoContent)
	})

	// API 404 fall-through
	rApi.PathPrefix("/").Handler(http.NotFoundHandler())

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/",
		http.FileServer(http.Dir(path.Join(server.ScriptDir(), "www")))))

	// Any other paths, just return index.html
	indexHtmlPath := path.Join(server.ScriptDir(), "www/index.html")
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.ServeFile(w, request, indexHtmlPath)
	})

	hWaitGroup := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		server.WaitGroupAdd(1)
		r.ServeHTTP(w, request)
		server.WaitGroupDone()
	})
	return http.Server{Handler: hWaitGroup}
}

func writeHttpJSON(w http.ResponseWriter, data interface{}) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	err := enc.Encode(data)
	if err == nil {
		prepareJSONHeaders(w)
		w.Write(buf.Bytes())
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func prepareJSONHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
}

func getProjectBuild(w http.ResponseWriter, request *http.Request, server types.Server) (projectBuild *types.ProjectBuild) {
	projectBuildId, err := types.ProjectBuildIdFromString(mux.Vars(request)["id"])
	if err != nil || projectBuildId <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	projectBuild, err = server.ProjectBuildById(projectBuildId)
	if projectBuild == nil || err != nil {
		w.WriteHeader(http.StatusNotFound)
	}

	return
}

func getProjectWithBuilds(project *types.Project, server types.Server) (out projectWithBuilds, err error) {
	projectBuilds, err := server.BuildsForProject(*project)
	if err != nil {
		return
	}
	projectBuildsWithProjectId := make([]projectBuildWithProjectId, len(projectBuilds))

	for i, projectBuild := range projectBuilds {
		projectBuildsWithProjectId[i] = projectBuildWithProjectId{ProjectBuild: projectBuild,
			Project: projectBuild.Project.Id}
	}

	return projectWithBuilds{Project: *project, ProjectBuilds: projectBuildsWithProjectId}, nil
}
