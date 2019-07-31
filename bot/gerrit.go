package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/trace"
	"github.com/nlopes/slack"
	"google.golang.org/api/iterator"
)

type (
	gerritCL struct {
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

	storedCL struct {
		Tweeted   bool      `datastore:"Tweeted,noindex"`
		URL       string    `datastore:"URL,noindex"`
		Message   string    `datastore:"Message,noindex"`
		CrawledAt time.Time `datastore:"CrawledAt"`
	}
)

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

func (b *Bot) getCLFromDS(ctx context.Context, query *datastore.Query) (*datastore.Key, *storedCL, error) {
	iter := b.dsClient.Run(ctx, query)

	dst := &storedCL{}
	key, err := iter.Next(dst)
	if err != nil && err != iterator.Done {
		b.logf("error while fetching history: %v\n", err)
		return nil, nil, err
	}

	return key, dst, nil
}

func (b *Bot) GetLastSeenCL(ctx context.Context) (int, error) {
	latestCLQuery := datastore.NewQuery("GoCL").
		Order("-CrawledAt").
		Limit(1).
		KeysOnly()

	key, _, err := b.getCLFromDS(ctx, latestCLQuery)
	if err != nil {
		return -1, err
	}
	if key == nil {
		return -1, nil
	}
	return int(key.ID), nil
}

func (b *Bot) wasShown(ctx context.Context, cl gerritCL) (bool, error) {
	key := datastore.IDKey("GoCL", int64(cl.Number), nil)
	query := datastore.NewQuery("GoCL").Ancestor(key)
	key, _, err := b.getCLFromDS(ctx, query)
	return key != nil, err
}

func (b *Bot) saveCL(ctx context.Context, cl gerritCL) error {
	taskKey := datastore.IDKey("GoCL", int64(cl.Number), nil)
	gocl := &storedCL{
		URL:       cl.link(),
		Message:   cl.message(),
		CrawledAt: time.Now(),
	}
	_, err := b.dsClient.Put(ctx, taskKey, gocl)
	return err
}

func (b *Bot) updateCL(ctx context.Context, key *datastore.Key, cl *storedCL) error {
	taskKey := datastore.IDKey("GoCL", key.ID, nil)
	_, err := b.dsClient.Put(ctx, taskKey, cl)
	return err
}

func (b *Bot) processCLList(ctx context.Context, lastID int, span *trace.Span) int {
	req, err := http.NewRequest("GET", b.gerritLink, nil)
	if err != nil {
		b.logf("failed to build GET request to %q: %v\n", b.gerritLink, err)
		return lastID
	}

	req.Header.Add("User-Agent", "Gophers Slack bot")
	req = req.WithContext(ctx)

	resp, err := b.client.Do(req)
	if err != nil {
		b.logf("failed to get data from Gerrit: %v\n", err)
		return lastID
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b.logf("got non-200 code: %d from gerrit api", resp.StatusCode)
		return lastID
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		b.logf("%s\n", err)
		return lastID
	}

	if len(body) < 4 {
		b.logf("got body: %s\n", string(body))
		return lastID
	}

	// Fix Gerrit adding a random prefix )]}'
	body = body[4:]
	cls := make([]gerritCL, 0)
	err = json.Unmarshal(body, &cls)
	if err != nil {
		b.logf("%s\n", err)
		return lastID
	}

	foundIdx := len(cls) - 1
	for idx := len(cls) - 1; idx >= 0; idx-- {
		if cls[idx].Number == lastID {
			foundIdx = idx
			break
		}
	}

	pubChannel := b.channels["golang-cls"].slackID
	if pubChannel[:1] == "#" {
		pubChannel = pubChannel[1:]
	}

	for idx := foundIdx - 1; idx >= 0; idx-- {
		cl := cls[idx]

		if shown, wserr := b.wasShown(ctx, cl); wserr == nil {
			if shown {
				continue
			}
		} else {
			b.logf("got error: %v\n", err)
			continue
		}

		err = b.saveCL(ctx, cl)
		if err != nil {
			b.logf("got error while saving CL to datastore: %v", err)
			return lastID
		}

		_, _, err = b.slackBotAPI.PostMessageContext(ctx, pubChannel,
			slack.MsgOptionAsUser(true),
			slack.MsgOptionText(fmt.Sprintf("[%d] %s: %s", cl.Number, cl.message(), cl.link()), false),
			slack.MsgOptionAttachments(slack.Attachment{
				Title:     cl.Subject,
				TitleLink: cl.link(),
				Text:      cl.Revisions[cl.CurrentRevision].Commit.Message,
				Footer:    cl.ChangeID,
			}),
		)
		if err != nil {
			b.logf("%s\n", err)
			continue
		}

		lastID = cl.Number
	}

	return lastID
}

// MonitorGerrit handles the Gerrit changes
func (b *Bot) MonitorGerrit(duration time.Duration) {
	tk := time.NewTicker(duration)
	defer tk.Stop()

	span := b.traceClient.NewSpan("b.MonitorGerrit")
	ctx := trace.NewContext(context.Background(), span)

	lastID, err := b.GetLastSeenCL(ctx)
	if err != nil {
		b.logf("got error while loading last ID from the datastore: %v\n", err)
		return
	}

	lastID = b.processCLList(ctx, lastID, span)
	span.Finish()
	for range tk.C {
		span = b.traceClient.NewSpan("b.processCLList")
		ctx := trace.NewContext(context.Background(), span)
		lastID = b.processCLList(ctx, lastID, span)
		span.Finish()
	}
}
