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

var branchRefPattern = regexp.MustCompile(`^refs/heads/(\w+)$`)

func NewHttpInterface(runJob chan JobRunRequest) (out http.Server) {
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
			w.WriteHeader(422) // Unprocessable Entity
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
		runJob <- req
		w.WriteHeader(204) // No Content

	})

	return http.Server{Handler: r}
}
