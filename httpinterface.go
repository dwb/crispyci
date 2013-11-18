package main

import (
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

		// TODO: list jobs
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
