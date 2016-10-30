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
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gopheracademy/gopher/bot"

	"github.com/gorilla/mux"
	"github.com/nlopes/slack"
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

	devMode := os.Getenv("GOPHERS_SLACK_BOT_DEV_MODE") == "true"

	if slackBotToken == "" {
		log.Fatal("slack bot token must be set in GOPHERS_SLACK_BOT_TOKEN")
	}

	if botName == "" {
		if devMode {
			log.Fatal("bot name must be set in GOPHERS_SLACK_BOT_NAME")
		}
		botName = "tempbot"
	}

	dsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	fmt.Printf("got env var GOOGLE_APPLICATION_CREDENTIALS=%s\n", dsFile)
	dsFileC, dsErr := ioutil.ReadFile(dsFile)
	fmt.Printf("got dserr: %v\n contents: %s\n", dsErr, string(dsFileC))

	slackBotAPI := slack.New(slackBotToken)

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

	slackBotRTM := slackBotAPI.NewRTM()
	go slackBotRTM.ManageConnection()
	runtime.Gosched()

	b := bot.NewBot(slackBotAPI, httpClient, gerritLink, botName, slackBotToken, botVersion, devMode, log.Printf)
	if err := b.Init(slackBotRTM); err != nil {
		panic(err)
	}

	go func() {
		<-time.After(1 * time.Second)
		b.MonitorGerrit(30 * time.Minute)
	}()

	go func() {
		for {
			select {
			case msg := <-slackBotRTM.IncomingEvents:
				switch message := msg.Data.(type) {
				case *slack.MessageEvent:
					go b.HandleMessage(message)

				case *slack.TeamJoinEvent:
					go b.TeamJoined(message)
				default:
					_ = message
				}
			}
		}

		log.Fatalln("should never be reached")
	}()

	go func() {
		r := mux.NewRouter()

		r.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, info)
		}).
			Name("info").
			Methods("GET")

		s := http.Server{
			Addr:         ":8081",
			Handler:      r,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		log.Fatal(s.ListenAndServe())
	}()

	log.Println("Gopher is now running")
	select {}
}
