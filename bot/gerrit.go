package bot

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/nlopes/slack"
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

	goCL struct {
		Tweeted   bool `datastore:",noindex"`
		CrawledAt time.Time
	}
)

func (b *Bot) MonitorGerrit(duration time.Duration) {
	tk := time.NewTicker(duration)
	defer tk.Stop()

	getCLFromDS := func(query *datastore.Query) (*datastore.Key, *goCL, error) {
		iter := b.dsClient.Run(b.ctx, query)

		dst := &goCL{}
		key, err := iter.Next(dst)
		if err != nil && err != datastore.Done {
			b.logf("error while fetching history: %v\n", err)
			return key, dst, err
		}

		return key, dst, nil
	}

	lastID, err := func() (int, error) {
		latestCLQuery := datastore.NewQuery("GoCL").
			Order("-CrawledAt").
			Limit(1).
			KeysOnly()

		key, _, err := getCLFromDS(latestCLQuery)
		if err != nil {
			return -1, err
		}
		if key == nil {
			return -1, nil
		}
		return int(key.ID()), nil
	}()

	if err != nil {
		b.logf("got error while loading last ID from the datastore: %v\n", err)
	}

	clLink := func(clNumber int) string {
		return fmt.Sprintf("https://go-review.googlesource.com/c/%d/", clNumber)
	}

	saveCL := func(cl gerritCL) error {
		taskKey := datastore.NewKey(b.ctx, "GoCL", "", int64(cl.Number), nil)
		gocl := &goCL{
			CrawledAt: time.Now(),
		}
		_, err := b.dsClient.Put(b.ctx, taskKey, gocl)
		return err
	}

	wasShown := func(cl gerritCL) bool {
		key := datastore.NewKey(b.ctx, "GoCL", "", int64(cl.Number), nil)
		query := datastore.NewQuery("GoCL").Ancestor(key)
		key, _, _ = getCLFromDS(query)
		return key != nil
	}

	pubChannel := b.channels["golang-cls"].slackID
	if pubChannel[:1] == "#" {
		pubChannel = pubChannel[1:]
	}

	processCLList := func(lastID int) int {
		req, err := http.NewRequest("GET", b.gerritLink, nil)
		req.Header.Add("User-Agent", "Gophers Slack bot")
		resp, err := b.client.Do(req)
		if err != nil {
			b.logf("%s\n", err)
			return lastID
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return lastID
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			b.logf("%s\n", err)
			return lastID
		}

		if len(body) < 4 {
			return lastID
		}

		// Fix Gerrit adding a random prefix )]}'
		body = body[4:]
		cls := []gerritCL{}
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

		for idx := foundIdx - 1; idx >= 0; idx-- {
			cl := cls[idx]
			if cl.Branch != "master" {
				continue
			}

			if wasShown(cl) {
				continue
			}

			msg := slack.Attachment{
				Title:     cl.Subject,
				TitleLink: clLink(cl.Number),
				Text:      cl.Revisions[cl.CurrentRevision].Commit.Message,
				Footer:    cl.ChangeID,
			}
			params := slack.PostMessageParameters{AsUser: true}
			params.Attachments = append(params.Attachments, msg)
			subject := cl.Subject
			if cl.Project != "go" {
				subject = fmt.Sprintf("[%s] %s", cl.Project, subject)
			}

			err = saveCL(cl)
			if err != nil {
				b.logf("got error while saving CL to datastore: %v", err)
				return lastID
			}

			_, _, err = b.slackBotAPI.PostMessage(b.channels["golang_cls"].slackID, fmt.Sprintf("%s: %s", subject, clLink(cl.Number)), params)
			if err != nil {
				b.logf("%s\n", err)
				continue
			}

			lastID = cl.Number

			_, _, err = b.slackBotAPI.PostMessage(pubChannel, fmt.Sprintf("%s: %s", subject, clLink(cl.Number)), params)
			if err != nil {
				b.logf("%s\n", err)
				continue
			}
		}

		return lastID
	}

	lastID = processCLList(lastID)
	for range tk.C {
		lastID = processCLList(lastID)
	}
}
