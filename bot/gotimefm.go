package bot

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/nlopes/slack"
)

type goTimeFMStatus struct {
	Streaming bool `json:"streaming"`
}

func (b *Bot) GoTimeFM() {
	ctx := context.Background()
	req, err := http.NewRequest("GET", "https://changelog.com/live/status", nil)
	req = req.WithContext(ctx)
	if err != nil {
		panic(err)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		panic(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		b.logf("got error while reading body for gotimefm %s", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		b.logf("got non-200 code from gotimefm: %d\n", resp.StatusCode)
		return
	}

	status := &goTimeFMStatus{}
	err = json.Unmarshal(body, status)
	if err != nil {
		b.logf("got error while unmarshalling gotimefm response: %s\n", err)
		return
	}

	timeNow := time.Now()
	if status.Streaming && timeNow.Sub(b.goTimeLastNotified).Hours() > 24 {
		b.goTimeLastNotified = timeNow
		response := ":tada: GoTimeFM is now live :tada:"
		params := slack.PostMessageParameters{AsUser: true}
		_, _, err := b.slackBotAPI.PostMessageContext(ctx, b.channels["gotimefm"].slackID, response, params)
		if err != nil {
			b.logf("got error while notifying slack: %s\n", err)
		}
		return
	}
}
