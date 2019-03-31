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
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gopheracademy/gopher/bot"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/trace"
	"github.com/ChimeraCoder/anaconda"
	"github.com/gorilla/mux"
	"github.com/nlopes/slack"
	"golang.org/x/net/context"
	"google.golang.org/api/option"
)

const gerritLink = "https://go-review.googlesource.com/changes/?q=status:merged&O=12&n=100"

var (
	botVersion = "HEAD"
	info       = `{ "version": "` + botVersion + `" }`
)

func main() {
	log.SetFlags(log.Lshortfile)

	botName := os.Getenv("GOPHERS_SLACK_BOT_NAME")
	slackBotToken := os.Getenv("GOPHERS_SLACK_BOT_TOKEN")
	twitterConsumerKey := os.Getenv("GOPHER_SLACK_BOT_TWITTER_CONSUMER_KEY")
	twitterConsumerSecret := os.Getenv("GOPHER_SLACK_BOT_TWITTER_CONSUMER_SECRET")
	twitterAccessToken := os.Getenv("GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN")
	twitterAccessTokenSecret := os.Getenv("GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN_SECRET")
	devMode := os.Getenv("GOPHERS_SLACK_BOT_DEV_MODE") == "true"

	if slackBotToken == "" {
		log.Fatalln("slack bot token must be set in GOPHERS_SLACK_BOT_TOKEN")
	}

	if botName == "" {
		if devMode {
			log.Fatalln("bot name must be set in GOPHERS_SLACK_BOT_NAME")
		}
		botName = "tempbot"
	}

	if twitterConsumerKey == "" {
		log.Fatalln("missing GOPHER_SLACK_BOT_TWITTER_CONSUMER_KEY")
	}

	if twitterConsumerSecret == "" {
		log.Fatalln("missing GOPHER_SLACK_BOT_TWITTER_CONSUMER_SECRET")
	}

	if twitterAccessToken == "" {
		log.Fatalln("missing GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN")
	}

	if twitterAccessTokenSecret == "" {
		log.Fatalln("missing GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN_SECRET")
	}

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

	ctx := context.Background()
	projectID := "gophers-slack-bot"

	traceClient, err := trace.NewClient(ctx, projectID, option.WithServiceAccountFile("/tmp/trace/trace.json"))

	startupSpan := traceClient.NewSpan("b.main")
	ctx = trace.NewContext(ctx, startupSpan)

	traceHttpClient := traceClient.NewHTTPClient(httpClient)

	slack.SetHTTPClient(traceHttpClient)
	slackBotAPI := slack.New(slackBotToken)

	botName = strings.TrimPrefix(botName, "@")

	anaconda.SetConsumerKey(twitterConsumerKey)
	anaconda.SetConsumerSecret(twitterConsumerSecret)
	twitterAPI := anaconda.NewTwitterApi(twitterAccessToken, twitterAccessTokenSecret)

	rtmOptions := &slack.RTMOptions{}
	slackBotRTM := slackBotAPI.NewRTMWithOptions(rtmOptions)
	go slackBotRTM.ManageConnection()
	runtime.Gosched()

	dsClient, err := datastore.NewClient(ctx, projectID, option.WithServiceAccountFile("/tmp/datastore/datastore.json"))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer dsClient.Close()

	b := bot.NewBot(slackBotAPI, dsClient, traceClient, twitterAPI, traceHttpClient, gerritLink, botName, slackBotToken, botVersion, devMode, log.Printf)
	if err := b.Init(ctx, slackBotRTM, startupSpan); err != nil {
		panic(err)
	}

	_, err = b.GetLastSeenCL(ctx)
	if err != nil {
		log.Printf("got error: %v\n", err)
		panic(err)
	}

	go func() {
		<-time.After(1 * time.Second)
		for i := 0; i < 7; i++ {
			b.MonitorGerrit(30 * time.Minute)
			log.Printf("monitoring Gerrit failed %d times\n", i+1)
			if i == 6 {
				break
			}
			time.Sleep(time.Duration(i*10) * time.Second)
		}
		panic("monitoring Gerrit was terminated")
	}()

	go func() {
		for msg := range slackBotRTM.IncomingEvents {
			switch message := msg.Data.(type) {
			case *slack.MessageEvent:
				go b.HandleMessage(message)

			case *slack.TeamJoinEvent:
				go b.TeamJoined(message)
			}
		}
	}()

	go func(traceClient *trace.Client) {
		healthz := func(traceClient *trace.Client) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				span := traceClient.SpanFromRequest(r)
				defer span.Finish()

				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, info)
			}
		}

		r := mux.NewRouter()

		r.HandleFunc("/healthz", healthz(traceClient)).
			Name("info").
			Methods("GET")

		s := http.Server{
			Addr:         ":8081",
			Handler:      r,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		log.Fatal(s.ListenAndServe())
	}(traceClient)

	go func() {
		gotimefm := time.NewTicker(1 * time.Minute)
		defer gotimefm.Stop()

		for range gotimefm.C {
			b.GoTimeFM()
		}
	}()

	log.Println("Gopher is now running")
	startupSpan.Finish()
	select {}
}
