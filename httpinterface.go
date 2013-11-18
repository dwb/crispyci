package main

import (
	"bytes"
	"encoding/json"
	"github.com/gorilla/mux"
	"net/http"
	"regexp"
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

const StatusUnprocessableEntity = 422

var branchRefPattern = regexp.MustCompile(`^refs/heads/(\w+)$`)

func NewHttpInterface(server *Server) (out http.Server) {
	r := mux.NewRouter()
	rApi := r.PathPrefix("/api/").Subrouter()

	r.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) {
		w.Write([]byte("So crisp!\n"))
		// TODO: return single-page HTML/JS page that talks to API
	})

	rApi.
		Methods("GET").
		Path("/jobs").
		HandlerFunc(func(w http.ResponseWriter, request *http.Request) {

		jobs, err := server.store.AllJobs()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		err = enc.Encode(jobs)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(buf.Bytes())
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
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

		err = server.store.WriteJob(&newJob)
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

	hWaitGroup := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		server.WaitGroupAdd(1)
		r.ServeHTTP(w, request)
		server.WaitGroupDone()
	})
	return http.Server{Handler: hWaitGroup}
}
