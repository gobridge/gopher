package gerrit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

const gerritURL = "https://go-review.googlesource.com/changes/?q=status:merged&O=12&n=100"

// TODO: this type contains more details than necessary for previous Twitter functionality.
type storedCL struct {
	Tweeted   bool      `datastore:"Tweeted,noindex"`
	URL       string    `datastore:"URL,noindex"`
	Message   string    `datastore:"Message,noindex"`
	CrawledAt time.Time `datastore:"CrawledAt"`
}

type gerritCL struct {
	Project         string `json:"project"`
	ChangeID        string `json:"change_id"`
	Number          int    `json:"_number"`
	Subject         string `json:"subject"`
	Branch          string `json:"branch"`
	CurrentRevision string `json:"current_revision"`
	Revisions       map[string]struct {
		Commit struct {
			Subject string `json:"subject"`
			Message string `json:"message"`
		} `json:"commit"`
	} `json:"revisions"`
}

func (cl *gerritCL) link() string {
	return fmt.Sprintf("https://golang.org/cl/%d/", cl.Number)
}

func (cl *gerritCL) message() string {
	subject := cl.Subject
	if cl.Project != "go" {
		subject = fmt.Sprintf("[%s] %s", cl.Project, subject)
	}

	return subject
}

// Gerrit tracks merged CLs.
type Gerrit struct {
	store  Store
	http   *http.Client
	logf   func(message string, args ...interface{})
	notify func(message string) bool

	lastID int
}

// Store persists information about CLs that have been handled.
type Store interface {
	LatestNumber(context.Context) (int, error)
	Put(_ context.Context, number int, _ storedCL) error
	Exists(_ context.Context, number int) (bool, error)
}

// ErrNotFound should be returned by Store implementations when CL number
// doesn't exist.
var ErrNotFound = errors.New("CL not found")

// New creates an initializes an instance of Gerrit.
func New(ctx context.Context, s Store, http *http.Client, logf func(message string, args ...interface{}), notify func(message string) bool) (*Gerrit, error) {
	lastID, err := s.LatestNumber(ctx)
	switch err {
	case nil:
	case ErrNotFound:
		lastID = -1
	default:
		return nil, fmt.Errorf("loading last ID from datastore: %v", err)
	}

	return &Gerrit{
		store:  s,
		http:   http,
		logf:   logf,
		notify: notify,
		lastID: lastID,
	}, nil
}

// Poll checks for new merged CLs and calls notify for each CL.
func (g *Gerrit) Poll(ctx context.Context) {
	req, err := http.NewRequest("GET", gerritURL, nil)
	if err != nil {
		g.logf("failed to build GET request to %q: %v\n", gerritURL, err)
		return
	}
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req = req.WithContext(ctx)

	resp, err := g.http.Do(req)
	if err != nil {
		g.logf("failed to get data from Gerrit: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.logf("got non-200 code: %d from gerrit api", resp.StatusCode)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		g.logf("reading body: %v", err)
		return
	}
	// Gerrit prefixes responses with `)]}'`
	// https://gerrit-review.googlesource.com/Documentation/rest-api.html#output
	body = bytes.TrimPrefix(body, []byte(")]}'"))

	var cls []gerritCL
	err = json.Unmarshal(body, &cls)
	if err != nil {
		g.logf("unmarshaling response: %v\n", err)
		return
	}

	// The change output is sorted by the last update time, most recently updated to oldest updated.
	// https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#list-changes
	for i, cl := range cls {
		if cl.Number == g.lastID {
			cls = cls[:i]
			break
		}
	}

	for i := len(cls) - 1; i >= 0; i-- {
		cl := cls[i]

		exists, err := g.store.Exists(ctx, cl.Number)
		if err != nil {
			g.logf("checking whether CL shown: %v", err)
			continue
		}
		if exists {
			continue
		}

		err = g.store.Put(ctx, cl.Number, storedCL{
			URL:       cl.link(),
			Message:   cl.message(),
			CrawledAt: time.Now(),
		})
		if err != nil {
			g.logf("saving CL to datastore: %v", err)
			return
		}

		msg := fmt.Sprintf("[%d] %s: %s", cl.Number, cl.message(), cl.link())
		if !g.notify(msg) {
			return
		}

		g.lastID = cl.Number
	}
}
