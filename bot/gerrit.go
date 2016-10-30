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
	"golang.org/x/net/context"
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
		Tweeted   bool      `datastore:"Tweeted,noindex"`
		URL       string    `datastore:"URL,noindex"`
		Message   string    `datastore:"Message,noindex"`
		CrawledAt time.Time `datastore:"CrawledAt"`
	}
)

func (cl *gerritCL) link() string {
	return fmt.Sprintf("https://go-review.googlesource.com/c/%d/", cl.Number)
}

func (cl *gerritCL) message() string {
	subject := cl.Subject
	if cl.Project != "go" {
		subject = fmt.Sprintf("[%s] %s", cl.Project, subject)
	}

	return subject
}

func (b *Bot) datastoreClient() (context.Context, *datastore.Client) {
	ctx := context.Background()
	projectID := "gopher-slack-bot"
	dsClient, err := datastore.NewClient(ctx, projectID)
	if err != nil {
		b.logf("Failed to create client: %v", err)
		panic(err)
	}

	return ctx, dsClient
}

func (b *Bot) getCLFromDS(ctx context.Context, dsClient *datastore.Client, query *datastore.Query) (*datastore.Key, *goCL, error) {
	iter := dsClient.Run(ctx, query)

	dst := &goCL{}
	key, err := iter.Next(dst)
	if err != nil && err != datastore.Done {
		b.logf("error while fetching history: %v\n", err)
		return nil, nil, err
	}

	return key, dst, nil
}

func (b *Bot) getLastSeenCL(ctx context.Context, dsClient *datastore.Client) (int, error) {
	latestCLQuery := datastore.NewQuery("GoCL").
		Order("-CrawledAt").
		Limit(1).
		KeysOnly()

	key, _, err := b.getCLFromDS(ctx, dsClient, latestCLQuery)
	if err != nil {
		return -1, err
	}
	if key == nil {
		return -1, nil
	}
	return int(key.ID()), nil
}

func (b *Bot) wasShown(ctx context.Context, dsClient *datastore.Client, cl gerritCL) (bool, error) {
	key := datastore.NewKey(ctx, "GoCL", "", int64(cl.Number), nil)
	query := datastore.NewQuery("GoCL").Ancestor(key)
	key, _, err := b.getCLFromDS(ctx, dsClient, query)
	return key != nil, err
}

func (b *Bot) saveCL(ctx context.Context, dsClient *datastore.Client, cl gerritCL) error {
	taskKey := datastore.NewKey(ctx, "GoCL", "", int64(cl.Number), nil)
	gocl := &goCL{
		URL:       cl.link(),
		Message:   cl.message(),
		CrawledAt: time.Now(),
	}
	_, err := dsClient.Put(ctx, taskKey, gocl)
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

	ctx, dsClient := b.datastoreClient()
	defer dsClient.Close()
	for idx := foundIdx - 1; idx >= 0; idx-- {
		cl := cls[idx]
		if cl.Branch != "master" {
			continue
		}

		if shown, err := b.wasShown(ctx, dsClient, cl); err == nil {
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

		err = b.saveCL(ctx, dsClient, cl)
		if err != nil {
			b.logf("got error while saving CL to datastore: %v", err)
			return lastID
		}

		_, _, err = b.slackBotAPI.PostMessage(b.channels["golang_cls"].slackID, fmt.Sprintf("%s: %s", cl.message(), cl.link()), params)
		if err != nil {
			b.logf("%s\n", err)
			continue
		}

		lastID = cl.Number

		_, _, err = b.slackBotAPI.PostMessage(pubChannel, fmt.Sprintf("%s: %s", cl.message(), cl.link()), params)
		if err != nil {
			b.logf("%s\n", err)
			continue
		}
	}

	return lastID
}

func (b *Bot) shareCL(event *slack.MessageEvent, eventText string) {
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

	clNumber, err := strconv.ParseInt(eventText, 10, 64)
	if err != nil {
		b.logf("could not convert string to int: %v from event: %#v\n", err, event)

		params := slack.PostMessageParameters{AsUser: true}
		_, _, err := b.slackBotAPI.PostMessage(event.User, `Could not share CL, please try again`, params)
		if err != nil {
			b.logf("%s\n", err)
		}
		return
	}

	ctx, dsClient := b.datastoreClient()
	defer dsClient.Close()

	key := datastore.NewKey(ctx, "GoCL", "", clNumber, nil)
	query := datastore.NewQuery("GoCL").Ancestor(key)
	key, cl, err := b.getCLFromDS(ctx, dsClient, query)
	if err != nil {
		b.logf("error while retriving CL from the DB: %v\n", err)

		params := slack.PostMessageParameters{AsUser: true}
		_, _, err := b.slackBotAPI.PostMessage(event.User, `Could not share CL, please try again`, params)
		if err != nil {
			b.logf("%s\n", err)
		}
		return
	}

	if cl.Tweeted {
		params := slack.PostMessageParameters{AsUser: true}
		_, _, err := b.slackBotAPI.PostMessage(event.User, `Already tweeted`, params)
		if err != nil {
			b.logf("%s\n", err)
		}
		return
	}

	message := cl.Message + " " + cl.URL
	_, err = b.twitterAPI.PostTweet(message, nil)
	if err != nil {
		b.logf("got error while tweeting CL: %d %#v\n", clNumber, err)

		params := slack.PostMessageParameters{AsUser: true}
		_, _, err := b.slackBotAPI.PostMessage(event.User, `Could not share CL, please try again`, params)
		if err != nil {
			b.logf("%s\n", err)
		}
		return
	}

	cl.Tweeted = true
	err = b.saveCL(ctx, dsClient, cl)
	if err != nil {
		b.logf("got error while updating CL to datastore: %v", err)

		params := slack.PostMessageParameters{AsUser: true}
		_, _, err := b.slackBotAPI.PostMessage(event.User, `Could not update tweet status in the DB`, params)
		if err != nil {
			b.logf("%s\n", err)
		}
	}
}

// MonitorGerrit handles the Gerrit changes
func (b *Bot) MonitorGerrit(duration time.Duration) {
	tk := time.NewTicker(duration)
	defer tk.Stop()

	ctx, dsClient := b.datastoreClient()

	lastID, err := b.getLastSeenCL(ctx, dsClient)
	if err != nil {
		b.logf("got error while loading last ID from the datastore: %v\n", err)
		dsClient.Close()
		return
	}
	dsClient.Close()

	lastID = b.processCLList(lastID)
	for range tk.C {
		lastID = b.processCLList(lastID)
	}
}
