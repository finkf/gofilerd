// Package api defines the base dataypes for the profilerd api.
package api

import (
	"github.com/finkf/gofiler"
)

// Version defines the version of the gofilerd api.
const Version = "1.0"

// Languages is the list of the available profiler languages. It the
// result for any [GET] profile/languages request.
type Languages struct {
	Languages []string // Available languages
}

// Token is a unique token to identify background profiling
// processes. It is returned for any [POST] profile requests. Use the
// tokens's unique ID to get/query the status of the associated
// profiling request: [GET] profile?token=Token.ID
type Token struct {
	ID string // Unique ID for the profiling token
}

// String returns the string representation of the token.
func (t Token) String() string {
	return t.ID
}

// Profile is the final result for a profiling request. For unfinished
// profiles, Done is false and the Profile is nil.
type Profile struct {
	Profile  gofiler.Profile // The profile
	Token    Token           // The profiling token id
	Language string          // The language
	Status   string          // Status string of the profiling
	Done     bool            // True if the profiling has finished
}

// Request is the post data structure to order a document
// profile.
type Request struct {
	Language string          // The language of the document
	Tokens   []gofiler.Token // Tokens of the document to profile
}
