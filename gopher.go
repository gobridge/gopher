// Copyright 2016 Florin Pățan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Command gopher
//
// This is a Slack bot for the Gophers Slack.
//
// You can get an invite from https://invite.slack.golangbridge.org/
//
// To run this you need to set the ` GOPHERS_SLACK_BOT_TOKEN ` environment
// variable with the Slack bot token and that's it.
package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gopheracademy/gopher/bot"

	"github.com/nlopes/slack"
)

var botVersion = "HEAD"

func main() {
	botName := os.Getenv("GOPHERS_SLACK_BOT_NAME")
	slackToken := os.Getenv("GOPHERS_SLACK_BOT_TOKEN")
	devMode := os.Getenv("GOPHERS_SLACK_BOT_DEV_MODE") == "true"

	if slackToken == "" {
		log.Fatal("slack token must be set in the GOPHERS_SLACK_BOT_TOKEN environment variable")
	}

	if botName == "" {
		if devMode {
			log.Fatal("bot name missing, set it with GOPHERS_SLACK_BOT_NAME")
		}
		botName = "tempbot"
	}

	slackAPI := slack.New(slackToken)

	httpClient := &http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	if strings.HasPrefix(botName, "@") {
		botName = botName[1:]
	}

	rtm := slackAPI.NewRTM()
	go rtm.ManageConnection()
	runtime.Gosched()

	b := bot.NewBot(slackAPI, httpClient, botName, slackToken, botVersion, devMode, log.Printf)
	if err := b.Init(rtm); err != nil {
		panic(err)
	}

	for {
		select {
		case msg := <-rtm.IncomingEvents:
			switch message := msg.Data.(type) {
			case *slack.MessageEvent:
				go b.HandleMessage(message)

			case *slack.TeamJoinEvent:
				go b.TeamJoined(message)
			default:
			}
		}
	}
}
