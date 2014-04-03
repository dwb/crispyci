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
	Projects []projectWithRuns `json:"projects"`
}

type projectShowResponse struct {
	Project projectWithRuns `json:"project"`
}

type projectWithRuns struct {
	types.Project
	ProjectRuns []projectRunWithProjectId `json:"projectRuns"`
}

type projectRunWithProjectId struct {
	types.ProjectRun
	Project types.ProjectId `json:"project"`
}

type projectRunShowResponse struct {
	ProjectRun projectRunWithProjectId `json:"projectRun"`
}

type projectRunUpdateResponse struct {
	ProjectRun  projectRunWithProjectId `json:"projectRun"`
	Deleted bool            `json:"deleted"`
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

		projectsWithRuns := make([]projectWithRuns, 0, len(projects))
		for _, project := range projects {
			projectRuns, err := getProjectWithRuns(&project, server)
			if err != nil {
				continue
			}
			projectsWithRuns = append(projectsWithRuns, projectRuns)
		}

		writeHttpJSON(w, projectsIndexResponse{Projects: projectsWithRuns})
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

		jr, err := getProjectWithRuns(project, server)
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
		Path("/projectRuns/updates").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		handleWebsocket(w, request, func(w http.ResponseWriter, request *http.Request, ws *websocket.Conn, wcn http.CloseNotifier) {

			projectRuns := server.SubProjectRunUpdates()
			defer server.Unsub(projectRuns)

			for {
				select {
				case projectRunI := <-projectRuns:
					projectRunUpdate, ok := projectRunI.(types.ProjectRunUpdate)
					if !ok {
						continue
					}

					resp := projectRunUpdateResponse{
						ProjectRun: projectRunWithProjectId{ProjectRun: projectRunUpdate.ProjectRun,
							Project: projectRunUpdate.Project.Id},
						Deleted: projectRunUpdate.Deleted}
					websocket.JSON.Send(ws, resp)
				case _ = <-wcn.CloseNotify():
					return
				}
			}
		})
	})

	rApi.
		Path("/projectRuns/{id}").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projectRun := getProjectRun(w, request, server)
		if projectRun == nil {
			return
		}

		writeHttpJSON(w, projectRunShowResponse{
			ProjectRun: projectRunWithProjectId{ProjectRun: *projectRun, Project: projectRun.Project.Id}})
	})

	rApi.
		Path("/projectRuns/{id}/progress").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projectRun := getProjectRun(w, request, server)
		if projectRun == nil {
			return
		}

		handleWebsocket(w, request, func(w http.ResponseWriter,
			request *http.Request, ws *websocket.Conn, wcn http.CloseNotifier) {

			progressChan, stopProgress := server.ProgressChanForProjectRun(*projectRun)
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
		Path("/projectRuns/{id}/abort").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		projectRun := getProjectRun(w, request, server)
		if projectRun == nil {
			return
		}

		projectRun.Abort()
		w.WriteHeader(http.StatusNoContent)
	})

	rApi.
		Methods("POST").
		Path("/github-post-receive").
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

		req := types.NewProjectRunRequest()
		req.ProjectName = name
		req.Source = "Github"
		server.SubmitProjectRunRequest(req)
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

func getProjectRun(w http.ResponseWriter, request *http.Request, server types.Server) (projectRun *types.ProjectRun) {
	projectRunId, err := types.ProjectRunIdFromString(mux.Vars(request)["id"])
	if err != nil || projectRunId <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	projectRun, err = server.ProjectRunById(projectRunId)
	if projectRun == nil || err != nil {
		w.WriteHeader(http.StatusNotFound)
	}

	return
}

func getProjectWithRuns(project *types.Project, server types.Server) (out projectWithRuns, err error) {
	projectRuns, err := server.RunsForProject(*project)
	if err != nil {
		return
	}
	projectRunsWithProjectId := make([]projectRunWithProjectId, len(projectRuns))

	for i, projectRun := range projectRuns {
		projectRunsWithProjectId[i] = projectRunWithProjectId{ProjectRun: projectRun,
			Project: projectRun.Project.Id}
	}

	return projectWithRuns{Project: *project, ProjectRuns: projectRunsWithProjectId}, nil
}
