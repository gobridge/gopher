package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

type (
	sentCL struct {
		ID              string `json:"change_id"`
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
)

var errNonOK = errors.New("non-ok status")

func (b *Bot) MonitorGerrit(duration time.Duration) {
	tk := time.NewTicker(duration)
	defer tk.Stop()

	lastID := ""

	historyParams := slack.HistoryParameters{Count: 100}
	history, err := b.slackBotAPI.GetGroupHistory(b.channels["golang_cls"].slackID, historyParams)
	if err != nil {
		b.log("error while fetching history: %v\n", err)
	} else {
		for _, msg := range history.Messages {
			if msg.User != b.id {
				continue
			}
			if len(msg.Attachments) != 1 {
				continue
			}
			lastID = strings.ToLower(msg.Attachments[0].Footer)
			break
		}
	}

	clLink := func(clNumber int) string {
		return fmt.Sprintf("https://go-review.googlesource.com/c/%d/", clNumber)
	}

	processStuff := func(lastID string) string {
		req, err := http.NewRequest("GET", b.gerritLink, nil)
		req.Header.Add("User-Agent", "Gophers Slack bot")
		resp, err := b.client.Do(req)
		if err != nil {
			b.log("%s\n", err)
			return lastID
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return lastID
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			b.log("%s\n", err)
			return lastID
		}

		if len(body) < 4 {
			return lastID
		}

		// Fix Gerrit adding a random prefix )]}'
		body = body[4:]
		cls := []sentCL{}
		err = json.Unmarshal(body, &cls)
		if err != nil {
			b.log("%s\n", err)
			return lastID
		}

		foundIdx := len(cls) - 1
		for idx := len(cls) - 1; idx >= 0; idx-- {
			if strings.ToLower(cls[idx].ID) == lastID {
				foundIdx = idx
				break
			}
		}

		for idx := foundIdx - 1; idx >= 0; idx-- {
			cl := cls[idx]
			lastID = strings.ToLower(cl.ID)
			msg := slack.Attachment{
				Title:     cl.Subject,
				TitleLink: clLink(cl.Number),
				Text:      cl.Revisions[cl.CurrentRevision].Commit.Message,
				Footer:    cl.ID,
			}
			params := slack.PostMessageParameters{AsUser: true}
			params.Attachments = append(params.Attachments, msg)
			_, _, err = b.slackBotAPI.PostMessage(b.channels["golang_cls"].slackID, msg.Fallback, params)
			if err != nil {
				b.log("%s\n", err)
				continue
			}
		}

		return lastID
	}

	lastID = processStuff(lastID)
	for range tk.C {
		lastID = processStuff(lastID)
	}
}
