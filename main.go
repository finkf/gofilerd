package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"strings"

	"github.com/finkf/gofiler"
	"github.com/finkf/gofilerd/api"
	log "github.com/sirupsen/logrus"
)

var (
	listen     string
	backend    string
	executable string
	timeout    uint
	maxJobs    uint
)

func init() {
	flag.StringVar(&listen, "listen", ":9998", "listen on host")
	flag.StringVar(&backend, "backend", "", "path to profiler's language backend")
	flag.StringVar(&executable, "profiler", "profiler", "path to the profiler executable")
	flag.UintVar(&timeout, "timeout", 45, "timeout for jobs (in minutes)")
	flag.UintVar(&maxJobs, "max-jobs", 10, "maximal number of pending jobs")
}

func main() {
	flag.Parse()
	log.SetLevel(log.DebugLevel)
	http.HandleFunc("/languages", withLogging(handle(withGet(getLanguages))))
	http.HandleFunc("/profile", withLogging(handle(withGetOrPost(
		withToken(getProfile),
		withRequest(withValidLanguage(profile))))))
	log.Infof("executable: %s", executable)
	log.Infof("backend:    %s", backend)
	log.Infof("timeout:    %dm", timeout)
	log.Infof("max-jobs:   %d", maxJobs)
	log.Infof("starting server listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, nil))
}

func withLogging(
	h func(http.ResponseWriter, *http.Request),
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Infof("handling request: [%s] %s", r.Method, r.URL)
		h(w, r)
	}
}

func withGet(
	h func(http.ResponseWriter, *http.Request) interface{},
) func(http.ResponseWriter, *http.Request) interface{} {
	return func(w http.ResponseWriter, r *http.Request) interface{} {
		if r.Method != http.MethodGet {
			return http.StatusMethodNotAllowed
		}
		return h(w, r)
	}
}

func withGetOrPost(
	get func(http.ResponseWriter, *http.Request) interface{},
	post func(http.ResponseWriter, *http.Request) interface{},
) func(http.ResponseWriter, *http.Request) interface{} {
	return func(w http.ResponseWriter, r *http.Request) interface{} {
		switch r.Method {
		case http.MethodGet:
			return get(w, r)
		case http.MethodPost:
			return post(w, r)
		default:
			return http.StatusMethodNotAllowed
		}
	}
}

func handle(
	h func(http.ResponseWriter, *http.Request) interface{},
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		x := h(w, r)
		switch t := x.(type) {
		case int:
			log.Infof("[%s] %s: status: %d (%s)",
				r.Method, r.URL, t, http.StatusText(t))
			http.Error(w, "", t)
		case error:
			log.Infof("[%s] %s: error: %v", r.Method, r.URL, t)
			http.Error(w, "", http.StatusInternalServerError)
		default:
			sendResponse(w, r, x)
		}
	}
}

// Send the response encoded as JSON.  Checks for errors and http
// Status flags.  If the client accepts gzipped data, the response
// objects returned as gzipped JSON.
func sendResponse(w http.ResponseWriter, r *http.Request, x interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Server", "gofilerd/"+api.Version)
	if containsVal(r.Header, "Accept-Encoding", "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		defer writer.Close()
		encodeJSON(writer, x)
		return
	}
	encodeJSON(w, x)
}

func containsVal(header http.Header, key, val string) bool {
	for _, v := range header[key] {
		if strings.Contains(v, val) {
			return true
		}
	}
	return false
}

func encodeJSON(w io.Writer, x interface{}) {
	if err := json.NewEncoder(w).Encode(x); err != nil {
		log.Infof("error: cannot write result: %v", err)
	}
}

// Check if the post request data is valid.  Decode post data.  Accept
// only application/json; charset=utf-8
func withRequest(
	h func(api.Request) interface{},
) func(http.ResponseWriter, *http.Request) interface{} {
	return func(w http.ResponseWriter, r *http.Request) interface{} {
		if !containsVal(r.Header, "Content-Type", "application/json") ||
			!containsVal(r.Header, "Content-Type", "charset=utf-8") {
			log.Infof("invalid Content-Type: %s", r.Header.Get("Content-Type"))
			return http.StatusBadRequest
		}
		if containsVal(r.Header, "Content-Encoding", "gzip") {
			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				log.Infof("cannot decode gzipped data: %v", err)
				return http.StatusBadRequest
			}
			defer reader.Close()
			return decodeJSON(reader, h)
		}
		return decodeJSON(r.Body, h)
	}
}

func decodeJSON(r io.Reader, h func(api.Request) interface{}) interface{} {
	var data api.Request
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		log.Infof("cannot decode request: %v", err)
		return http.StatusBadRequest
	}
	return h(data)
}

// Check if the requested language is valid.
func withValidLanguage(
	h func(string, api.Request) interface{},
) func(api.Request) interface{} {
	return func(request api.Request) interface{} {
		lc, err := gofiler.FindLanguage(backend, request.Language)
		if err == gofiler.ErrorLanguageNotFound {
			return http.StatusNotFound
		}
		if err != nil {
			return err
		}
		return h(lc.Path, request)
	}
}

func withToken(
	h func(api.Token) interface{},
) func(http.ResponseWriter, *http.Request) interface{} {
	return func(w http.ResponseWriter, r *http.Request) interface{} {
		id := r.URL.Query().Get("token")
		if id == "" {
			return http.StatusBadRequest
		}
		return h(api.Token{ID: id})
	}
}

func getLanguages(w http.ResponseWriter, r *http.Request) interface{} {
	lcs, err := gofiler.ListLanguages(backend)
	if err != nil {
		return err
	}
	var ls api.Languages
	for _, lc := range lcs {
		ls.Languages = append(ls.Languages, lc.Language)
	}
	return ls
}
