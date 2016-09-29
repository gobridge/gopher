package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/nlopes/slack"
	"strings"
)

type commit struct {
	ID      string `json:"change_id"`
	Number  int    `json:"_number"`
	Subject string `json:"subject"`
	Branch  string `json:"branch"`
}

var errNonOK = errors.New("non-ok status")

func (b *Bot) MonitorGerrit() {
	tk := time.NewTicker(1 * time.Hour)
	defer tk.Stop()

	lastID := ""

	historyParams := slack.HistoryParameters{Count: 100}
	history, err := b.slackAPI.GetChannelHistory(b.channels["golang_cls"], historyParams)
	if err != nil {
		b.log(err)
	} else {
		for _, msg := range history.Messages {
			if msg.BotID != b.id {
				continue
			}
			parts := strings.Split(strings.ToLower(msg.Text), " #-# ")
			if len(parts) != 3 {
				continue
			}
			lastID = parts[1]
			break
		}
	}

	params := slack.PostMessageParameters{AsUser: true}
	for <-tk.C {
		req, err := http.NewRequest("GET", b.gerritLink, nil)
		req.Header.Add("User-Agent", "Gophers Slack bot")
		resp, err := b.client.Do(req)
		if err != nil {
			b.log("%s\n", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.log("%s\n", err)
			continue
		}

		// Fix Gerrit adding a random prefix )]}'
		body = body[4:]
		cls := []commit{}
		err = json.Unmarshal(body, &cls)
		if err != nil {
			b.log("%s\n", err)
			continue
		}

		for _, cl := range cls {
			if cl.ID == lastID {
				break
			}
			clLine := fmt.Sprintf("%d #-# %s #-# %s", cl.Number, cl.ID, cl.Subject)
			_, _, err = b.slackAPI.PostMessage(b.channels["golang_cls"].slackID, clLine, params)
			if err != nil {
				b.log("%s\n", err)
				continue
			}
		}
	}
}
