package main

import (
	"bytes"
	"encoding/json"
	"github.com/gorilla/mux"
	"net/http"
	"path"
	"regexp"
	"strconv"
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
	Job
	JobRuns []JobRun `json:"jobRuns"`
}

const StatusUnprocessableEntity = 422

var branchRefPattern = regexp.MustCompile(`^refs/heads/(\w+)$`)

func NewHttpInterface(server *Server) (out http.Server) {
	r := mux.NewRouter()
	rApi := r.PathPrefix("/api/v1/").Subrouter()

	rApi.
		Methods("GET").
		Path("/jobs").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobs, err := server.store.AllJobs()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		jobsWithRuns := make([]jobWithRuns, 0, len(jobs))
		for _, job := range jobs {
			jobRuns, err := server.store.RunsForJob(job)
			if err != nil {
				continue
			}
			jobsWithRuns = append(jobsWithRuns,
				jobWithRuns{Job: job, JobRuns: jobRuns})
		}

		writeHttpJSON(w, jobsIndexResponse{Jobs: jobsWithRuns})
	})

	rApi.
		Methods("GET").
		Path("/jobs/{id}").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobId, err := strconv.ParseUint(mux.Vars(request)["id"], 10, 64)
		if err != nil || jobId <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		job, err := server.store.JobById(JobId(jobId))
		if err != nil || job == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		jobRuns, err := server.store.RunsForJob(*job)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		writeHttpJSON(w, jobShowResponse{Job: jobWithRuns{Job: *job, JobRuns: jobRuns}})
	})

	rApi.
		Methods("POST").
		Path("/jobs").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		dec := json.NewDecoder(request.Body)
		newJob := NewJob()
		err := dec.Decode(&newJob)
		if err != nil {
			w.WriteHeader(StatusUnprocessableEntity)
			w.Write([]byte("JSON parse error\n"))
			return
		}

		// TODO: input validation

		err = server.store.WriteJob(newJob)
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
		Methods("POST").
		Path("/github-post-receive").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		dec := json.NewDecoder(request.Body)
		var payload githubWebhookPayload
		err := dec.Decode(&payload)
		if err != nil {
			w.WriteHeader(StatusUnprocessableEntity)
			return
		}

		name := payload.Repository.Name
		m := branchRefPattern.FindStringSubmatch(payload.Ref)
		if m != nil {
			name += "-" + m[1]
		}

		req := NewJobRunRequest()
		req.JobName = name
		req.Source = "Github"
		server.SubmitJobRunRequest(req)
		w.WriteHeader(http.StatusNoContent)
	})

	// API 404 fall-through
	rApi.PathPrefix("/").Handler(http.NotFoundHandler())

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/",
		http.FileServer(http.Dir(path.Join(server.scriptDir, "www")))))

	// Any other paths, just return index.html
	indexHtmlPath := path.Join(server.scriptDir, "www/index.html")
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
