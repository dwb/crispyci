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

type jobsIndexResponse struct {
	Jobs []jobWithRuns `json:"jobs"`
}

type jobShowResponse struct {
	Job jobWithRuns `json:"job"`
}

type jobWithRuns struct {
	types.Job
	JobRuns []jobRunWithJobId `json:"jobRuns"`
}

type jobRunWithJobId struct {
	types.JobRun
	Job types.JobId `json:"job"`
}

type jobRunShowResponse struct {
	JobRun jobRunWithJobId `json:"jobRun"`
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
		Path("/jobs").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobs, err := server.AllJobs()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		jobsWithRuns := make([]jobWithRuns, 0, len(jobs))
		for _, job := range jobs {
			jobRuns, err := getJobWithRuns(&job, server)
			if err != nil {
				continue
			}
			jobsWithRuns = append(jobsWithRuns, jobRuns)
		}

		writeHttpJSON(w, jobsIndexResponse{Jobs: jobsWithRuns})
	})

	rApi.
		Methods("GET").
		Path("/jobs/{id}").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobId, err := types.JobIdFromString(mux.Vars(request)["id"])
		if err != nil || jobId <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		job, err := server.JobById(types.JobId(jobId))
		if err != nil || job == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		jr, err := getJobWithRuns(job, server)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		writeHttpJSON(w, jobShowResponse{Job: jr})
	})

	rApi.
		Methods("POST").
		Path("/jobs").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		dec := json.NewDecoder(request.Body)
		newJob := types.NewJob()
		err := dec.Decode(&newJob)
		if err != nil {
			w.WriteHeader(StatusUnprocessableEntity)
			w.Write([]byte("JSON parse error\n"))
			return
		}

		// TODO: input validation

		err = server.WriteJob(newJob)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			// TODO: better error message / logging
			w.Write([]byte("Save error\n"))
			return
		}

		// TODO: location header for new job
		w.WriteHeader(http.StatusCreated)
	})

	rApi.
		Path("/jobRuns/updates").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		handleWebsocket(w, request, func(w http.ResponseWriter, request *http.Request, ws *websocket.Conn, wcn http.CloseNotifier) {

			jobRuns := server.SubJobRunUpdates()
			defer server.Unsub(jobRuns)

			for {
				select {
				case jobRunI := <-jobRuns:
					jobRun, ok := jobRunI.(types.JobRun)
					if !ok {
						continue
					}

					resp := jobRunShowResponse{
						JobRun: jobRunWithJobId{JobRun: jobRun,
							Job: jobRun.Job.Id}}
					websocket.JSON.Send(ws, resp)
				case _ = <-wcn.CloseNotify():
					return
				}
			}
		})
	})

	rApi.
		Path("/jobRuns/{id}").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobRun := getJobRun(w, request, server)
		if jobRun == nil {
			return
		}

		writeHttpJSON(w, jobRunShowResponse{
			JobRun: jobRunWithJobId{JobRun: *jobRun, Job: jobRun.Job.Id}})
	})

	rApi.
		Path("/jobRuns/{id}/progress").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobRun := getJobRun(w, request, server)
		if jobRun == nil {
			return
		}

		handleWebsocket(w, request, func(w http.ResponseWriter,
			request *http.Request, ws *websocket.Conn, wcn http.CloseNotifier) {

			progressChan, stopProgress := server.ProgressChanForJobRun(*jobRun)
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
		Path("/jobRuns/{id}/abort").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobRun := getJobRun(w, request, server)
		if jobRun == nil {
			return
		}

		jobRun.Abort()
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

		req := types.NewJobRunRequest()
		req.JobName = name
		req.Source = "Github"
		server.SubmitJobRunRequest(req)
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

func getJobRun(w http.ResponseWriter, request *http.Request, server types.Server) (jobRun *types.JobRun) {
	jobRunId, err := types.JobRunIdFromString(mux.Vars(request)["id"])
	if err != nil || jobRunId <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	jobRun, err = server.JobRunById(jobRunId)
	if jobRun == nil || err != nil {
		w.WriteHeader(http.StatusNotFound)
	}

	return
}

func getJobWithRuns(job *types.Job, server types.Server) (out jobWithRuns, err error) {
	jobRuns, err := server.RunsForJob(*job)
	if err != nil {
		return
	}
	jobRunsWithJobId := make([]jobRunWithJobId, len(jobRuns))

	for i, jobRun := range jobRuns {
		jobRunsWithJobId[i] = jobRunWithJobId{JobRun: jobRun,
			Job: jobRun.Job.Id}
	}

	return jobWithRuns{Job: *job, JobRuns: jobRunsWithJobId}, nil
}
