package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/finkf/gofiler"
	"github.com/finkf/gofilerd/api"
	log "github.com/sirupsen/logrus"
)

func init() {
	rand.Seed(time.Now().Unix())
}

var (
	verbs = []string{
		"eating", "smelling", "seeing", "kicking", "liking", "tasting", "licking",
	}
	adjectives = []string{
		"sweet", "old", "dead", "tiny", "small", "bitter", "cold",
	}
	nouns = []string{
		"pancakes", "farts", "people", "kittens", "feet", "ashes", "steel",
	}
	jobs jobMap
)

type result struct {
	profile gofiler.Profile
	err     error
}

type job struct {
	pending  <-chan result
	language string
	start    time.Time
}

type jobMap struct {
	m map[string]job
	l sync.RWMutex
}

// Check for an entry in the map.
func (m *jobMap) get(token string) (job, bool) {
	m.l.RLock()
	defer m.l.RUnlock()
	pp, ok := m.m[token]
	return pp, ok
}

// Delete an entry from the map. The according channel is not closed
// (the writer of the channel is supposed to do this).
func (m *jobMap) del(token string) {
	m.l.Lock()
	defer m.l.Unlock()
	delete(m.m, token)
}

const (
	putJobOK int = iota
	putJobNotUnique
	putJobFull
)

// Insert a new unique entry into the map.  If the entry was unique
// and could be put into the map, putJobOK is returned.  Otherwise if
// the token is not unique, putJobNotUnique is returned.  If the map
// is full, putJobFull is returend.
func (m *jobMap) put(language, token string, pchan <-chan result) int {
	// make sure that no one writes into the map
	m.l.Lock()
	defer m.l.Unlock()
	if m.m == nil {
		m.m = make(map[string]job)
	}
	// check if the map is full
	if len(m.m) >= int(maxJobs) {
		return putJobFull
	}
	// check if the map entry already exists
	_, ok := m.m[token]
	if ok {
		return putJobNotUnique
	}
	m.m[token] = job{
		pending:  pchan,
		language: language,
		start:    time.Now(),
	}
	return putJobOK
}

func (m *jobMap) clean() {
	m.l.Lock()
	defer m.l.Unlock()

	// search for timed out jobs
	var forDeletion []string
	delta := time.Duration(timeout) * time.Minute
	now := time.Now()
	for token, job := range m.m {
		if now.After(job.start.Add(delta)) {
			forDeletion = append(forDeletion, token)
		}
	}
	// delete timed out jobs
	for _, token := range forDeletion {
		log.Debugf("deleting job %s started at: %s",
			token, m.m[token].start)
		delete(m.m, token)
	}
}

// Check if the job specified by the given token is done and return
// the profile if its done.
func getProfile(token api.Token) interface{} {
	job, ok := jobs.get(token.ID)
	if !ok {
		return http.StatusNotFound
	}
	// check if result for the token is available
	select {
	case p := <-job.pending:
		defer func() { jobs.del(token.ID) }()
		if p.err != nil {
			return p.err
		}
		log.Infof("job %v is done", token)
		return api.Profile{
			Profile:  p.profile,
			Status:   "done",
			Language: job.language,
			Token:    token,
			Done:     true,
		}
	}
	// profile is not available yet
	log.Infof("job %s is not done yet", token)
	return api.Profile{
		Status: fmt.Sprintf("%s %s %s",
			verbs[rand.Intn(len(verbs))],
			adjectives[rand.Intn(len(adjectives))],
			nouns[rand.Intn(len(nouns))]),
		Done:  false,
		Token: token,
	}
}

// Insert the job into the jobs map using a unique ID. Then start the
// job in the background. The result is read from the channel in the
// accorant GET /profile?token=ID request.
func profile(path string, request api.Request) interface{} {
	pchan := make(chan result)
	var token api.Token
	jobs.clean()
	for {
		token.ID = generateRandomID()
		res := jobs.put(request.Language, token.ID, pchan)
		switch res {
		case putJobOK:
			// We have a job. Start running it.
			log.Infof("starting job %s", token.ID)
			go runProfiler(path, request.Tokens, pchan)
			return token
		case putJobFull:
			log.Infof("cannot accept more jobs")
			return http.StatusServiceUnavailable
		}
	}
}

// Run the profiler and insert the result into the channel.
func runProfiler(config string, tokens []gofiler.Token, pchan chan<- result) {
	defer close(pchan)
	// make sure to defer cancel before channel can be read
	p, err := func() (gofiler.Profile, error) {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			time.Duration(timeout)*time.Minute,
		)
		defer cancel()
		return gofiler.Run(ctx, executable, config, tokens, logger{})
	}()
	log.Infof("profiled %d tokens with config %s", len(tokens), config)
	pchan <- result{profile: p, err: err}
}

var letters = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func generateRandomID() string {
	id := make([]byte, 16)
	for i := 0; i < len(id); i++ {
		id[i] = letters[rand.Intn(len(letters))]
	}
	return string(id)
}

type logger struct{}

func (logger) Log(str string) {
	log.Debug(str)
}
