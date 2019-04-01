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
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gobridge/gopher/bot"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/trace"
	"github.com/ChimeraCoder/anaconda"
	"github.com/gorilla/mux"
	"github.com/nlopes/slack"
	"golang.org/x/net/context"
	"google.golang.org/api/option"
)

const (
	gerritLink            = "https://go-review.googlesource.com/changes/?q=status:merged&O=12&n=100"
	defaultCredentialFile = "/tmp/trace/trace.json" // Also /tmp/datastore/datastore.json :-(
)

var (
	BotVersion = "HEAD"
	info       = `{ "version": "` + BotVersion + `" }`
)

func decodeGoogleCredentialsToFile(ec string) string {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(ec))
	b, err := ioutil.ReadAll(r)
	if err != nil {
		log.Fatalln("Unable to decode google credentials:", err)
	}
	fi, err := ioutil.TempFile("", "google_*")
	if err != nil {
		log.Fatalln("Unable to create temporary credential file:", err)
	}
	if n, err := fi.Write(b); err != nil || n != len(b) {
		log.Fatalln("Unable to completely write credential file:", err)
	}
	if err := fi.Close(); err != nil {
		log.Fatalln("Unable to close temporary credential file: ", err)
	}
	return fi.Name()
}

func main() {
	log.SetFlags(log.Lshortfile)

	botName := os.Getenv("GOPHERS_SLACK_BOT_NAME")
	slackBotToken := os.Getenv("GOPHERS_SLACK_BOT_TOKEN")
	twitterConsumerKey := os.Getenv("GOPHER_SLACK_BOT_TWITTER_CONSUMER_KEY")
	twitterConsumerSecret := os.Getenv("GOPHER_SLACK_BOT_TWITTER_CONSUMER_SECRET")
	twitterAccessToken := os.Getenv("GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN")
	twitterAccessTokenSecret := os.Getenv("GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN_SECRET")
	googleCredentials := os.Getenv("GOOGLE_CREDENTIALS")
	googleProjectID := os.Getenv("GOOGLE_PROJECT_ID")
	opsChannel := os.Getenv("OPS_CHANNEL")
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

	twitter := true
	if twitterConsumerKey == "" {
		log.Println("missing GOPHER_SLACK_BOT_TWITTER_CONSUMER_KEY; Twitter support disabled.")
		twitter = false
	}

	if twitterConsumerSecret == "" {
		log.Println("missing GOPHER_SLACK_BOT_TWITTER_CONSUMER_SECRET; Twitter support disabled.")
		twitter = false
	}

	if twitterAccessToken == "" {
		log.Println("missing GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN; Twitter support disabled.")
		twitter = false
	}

	if twitterAccessTokenSecret == "" {
		log.Println("missing GOPHER_SLACK_BOT_TWITTER_ACCESS_TOKEN_SECRET; Twitter support disabled.")
		twitter = false
	}

	if googleCredentials == "" {
		// FIXME: This doesn't deal with the default credentials in per service locations.

		if _, err := os.Stat(defaultCredentialFile); err == nil {
			googleCredentials = defaultCredentialFile
		}
	} else {
		googleCredentials = decodeGoogleCredentialsToFile(googleCredentials)
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

	traceClient, err := trace.NewClient(ctx, googleProjectID, option.WithServiceAccountFile(googleCredentials))
	if err != nil {
		log.Fatalln("Unable to create trace client:", err)
	}

	startupSpan := traceClient.NewSpan("b.main")
	ctx = trace.NewContext(ctx, startupSpan)

	traceHttpClient := traceClient.NewHTTPClient(httpClient)

	slack.SetHTTPClient(traceHttpClient)
	slackBotAPI := slack.New(slackBotToken)

	botName = strings.TrimPrefix(botName, "@")

	var twitterAPI *anaconda.TwitterApi
	if twitter {
		anaconda.SetConsumerKey(twitterConsumerKey)
		anaconda.SetConsumerSecret(twitterConsumerSecret)
		twitterAPI = anaconda.NewTwitterApi(twitterAccessToken, twitterAccessTokenSecret)
	}

	rtmOptions := &slack.RTMOptions{}
	slackBotRTM := slackBotAPI.NewRTMWithOptions(rtmOptions)
	go slackBotRTM.ManageConnection()
	runtime.Gosched()

	dsClient, err := datastore.NewClient(ctx, googleProjectID, option.WithServiceAccountFile(googleCredentials))
	if err != nil {
		log.Fatalln("Unable to create datastore client:", err)
	}
	defer dsClient.Close()

	b := bot.NewBot(slackBotAPI, dsClient, traceClient, twitterAPI, traceHttpClient, gerritLink, botName, slackBotToken, BotVersion, devMode, log.Printf)
	if err := b.Init(ctx, slackBotRTM, startupSpan, opsChannel); err != nil {
		log.Fatalln("Unable to init bot:", err)
	}

	_, err = b.GetLastSeenCL(ctx)
	if err != nil {
		panic("Unable to GetLastSeenCL: " + err.Error())
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

		port := os.Getenv("PORT")
		if port == "" {
			port = "8081"
		}

		s := http.Server{
			Addr:         ":" + port,
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
