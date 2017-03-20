package bot

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/datastore"
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

func (b *Bot) getCLFromDS(query *datastore.Query) (*datastore.Key, *storedCL, error) {
	iter := b.dsClient.Run(b.ctx, query)

	dst := &storedCL{}
	key, err := iter.Next(dst)
	if err != nil && err != iterator.Done {
		b.logf("error while fetching history: %v\n", err)
		return nil, nil, err
	}

	return key, dst, nil
}

func (b *Bot) GetLastSeenCL() (int, error) {
	latestCLQuery := datastore.NewQuery("GoCL").
		Order("-CrawledAt").
		Limit(1).
		KeysOnly()

	key, _, err := b.getCLFromDS(latestCLQuery)
	if err != nil {
		return -1, err
	}
	if key == nil {
		return -1, nil
	}
	return int(key.ID), nil
}

func (b *Bot) wasShown(cl gerritCL) (bool, error) {
	key := datastore.IDKey("GoCL", int64(cl.Number), nil)
	query := datastore.NewQuery("GoCL").Ancestor(key)
	key, _, err := b.getCLFromDS(query)
	return key != nil, err
}

func (b *Bot) saveCL(cl gerritCL) error {
	taskKey := datastore.IDKey("GoCL", int64(cl.Number), nil)
	gocl := &storedCL{
		URL:       cl.link(),
		Message:   cl.message(),
		CrawledAt: time.Now(),
	}
	_, err := b.dsClient.Put(b.ctx, taskKey, gocl)
	return err
}

func (b *Bot) updateCL(key *datastore.Key, cl *storedCL) error {
	taskKey := datastore.IDKey("GoCL", key.ID, nil)
	_, err := b.dsClient.Put(b.ctx, taskKey, cl)
	return err
}

func (b *Bot) processCLList(lastID int) int {
	req, err := http.NewRequest("GET", b.gerritLink, nil)
	req.Header.Add("User-Agent", "Gophers Slack bot")
	resp, err := b.client.Do(req)
	if err != nil {
		b.logf("%s\n", err)
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

	pubChannel := b.channels["golang-cls"].slackID
	if pubChannel[:1] == "#" {
		pubChannel = pubChannel[1:]
	}

	pvtChannel := b.channels["golang_cls"].slackID

	for idx := foundIdx - 1; idx >= 0; idx-- {
		cl := cls[idx]

		if shown, err := b.wasShown(cl); err == nil {
			if shown {
				continue
			}
		} else {
			b.logf("got error: %v\n", err)
			continue
		}

		msg := slack.Attachment{
			Title:     cl.Subject,
			TitleLink: cl.link(),
			Text:      cl.Revisions[cl.CurrentRevision].Commit.Message,
			Footer:    cl.ChangeID,
		}
		params := slack.PostMessageParameters{AsUser: true}
		params.Attachments = append(params.Attachments, msg)

		err = b.saveCL(cl)
		if err != nil {
			b.logf("got error while saving CL to datastore: %v", err)
			return lastID
		}

		_, _, err = b.slackBotAPI.PostMessage(pvtChannel, fmt.Sprintf("[%d] %s: %s", cl.Number, cl.message(), cl.link()), params)
		if err != nil {
			b.logf("%s\n", err)
			continue
		}

		lastID = cl.Number

		_, _, err = b.slackBotAPI.PostMessage(pubChannel, fmt.Sprintf("[%d] %s: %s", cl.Number, cl.message(), cl.link()), params)
		if err != nil {
			b.logf("%s\n", err)
			continue
		}
	}

	return lastID
}

func shareCL(b *Bot, event *slack.MessageEvent) {
	// repeats some earlier work but oh well
	eventText := strings.Trim(strings.ToLower(event.Text), " \n\r")
	eventText = b.trimBot(event.Text)
	eventText = strings.TrimPrefix(eventText, "xkcd:")

	if !b.specialRestrictions("golang_cls", event) {
		b.logf("share attempt caught: %#v\n", event)

		params := slack.PostMessageParameters{AsUser: true}
		_, _, err := b.slackBotAPI.PostMessage(event.User, `You are not authorized to share CLs`, params)
		if err != nil {
			b.logf("%s\n", err)
		}
		return
	}

	eventText = strings.Replace(eventText, "share cl", "", -1)
	eventText = strings.Trim(eventText, " \n")

	for _, text := range strings.Fields(eventText) {
		clNumber, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			b.logf("could not convert string to int: %v from event: %#v\n", err, event)

			params := slack.PostMessageParameters{AsUser: true}
			_, _, err := b.slackBotAPI.PostMessage(event.User, fmt.Sprintf(`Could not share CL %d, please try again`, clNumber), params)
			if err != nil {
				b.logf("%s\n", err)
			}
			continue
		}

		key := datastore.IDKey("GoCL", clNumber, nil)
		query := datastore.NewQuery("GoCL").Ancestor(key)
		key, cl, err := b.getCLFromDS(query)
		if err != nil {
			b.logf("error while retriving CL from the DB: %v\n", err)

			params := slack.PostMessageParameters{AsUser: true}
			_, _, err := b.slackBotAPI.PostMessage(event.User, fmt.Sprintf(`Could not share CL %d, please try again`, clNumber), params)
			if err != nil {
				b.logf("%s\n", err)
			}
			continue
		}

		if cl.Tweeted {
			params := slack.PostMessageParameters{AsUser: true}
			_, _, err := b.slackBotAPI.PostMessage(event.User, fmt.Sprintf(`Already tweeted CL %d`, clNumber), params)
			if err != nil {
				b.logf("%s\n", err)
			}
			continue
		}

		message := cl.Message + " " + cl.URL
		_, err = b.twitterAPI.PostTweet(message, nil)
		if err != nil {
			b.logf("got error while tweeting CL: %d %#v\n", clNumber, err)

			params := slack.PostMessageParameters{AsUser: true}
			_, _, err := b.slackBotAPI.PostMessage(event.User, fmt.Sprintf(`Could not share CL %d, please try again`, clNumber), params)
			if err != nil {
				b.logf("%s\n", err)
			}
			continue
		}

		cl.Tweeted = true
		err = b.updateCL(key, cl)
		if err != nil {
			b.logf("got error while updating CL to datastore: %v", err)

			params := slack.PostMessageParameters{AsUser: true}
			_, _, err := b.slackBotAPI.PostMessage(event.User, fmt.Sprintf(`Could not update tweet status for CL %d in the DB`, clNumber), params)
			if err != nil {
				b.logf("%s\n", err)
			}
		}
	}
}

// MonitorGerrit handles the Gerrit changes
func (b *Bot) MonitorGerrit(duration time.Duration) {
	tk := time.NewTicker(duration)
	defer tk.Stop()

	lastID, err := b.GetLastSeenCL()
	if err != nil {
		b.logf("got error while loading last ID from the datastore: %v\n", err)
		return
	}

	lastID = b.processCLList(lastID)
	for range tk.C {
		lastID = b.processCLList(lastID)
	}
}
