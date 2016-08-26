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
	"bytes"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nlopes/slack"
	"fmt"
)

type slackChan struct {
	description string
	slackID     string
}

var (
	botName     = os.Getenv("GOPHERS_SLACK_BOT_NAME")
	botID       = ""
	slackToken  = os.Getenv("GOPHERS_SLACK_BOT_TOKEN")
	devMode     = os.Getenv("GOPHERS_SLACK_BOT_DEV_MODE")
	botVersion = "HEAD"
	slackAPI    = slack.New(slackToken)
	emojiRE     = regexp.MustCompile(`:[[:alnum:]]+:`)
	slackLinkRE = regexp.MustCompile(`<((?:@u)|(?:#c))[0-9a-z]+>`)

	channels = map[string]slackChan{
		"golang-newbies": {description: "for newbie resources"},
		"reviews":        {description: "for code reviews"},
		"showandtell":    {description: "tell the world about the thing you are working on"},
		"golang-jobs":    {description: "for jobs related to Go"},
		// TODO add more channels to share with the newbies?
	}
)

func init() {
	if slackToken == "" {
		log.Fatal("slack token must be set in the GOPHERS_SLACK_BOT_TOKEN environment variable")
	}

	if botName == "" {
		if devMode != "true" {
			log.Fatal("bot name missing, set it with GOPHERS_SLACK_BOT_NAME")
		}
		botName = "tempbot"
	}

	if strings.HasPrefix(botName, "@") {
		botName = botName[1:]
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		log.Println("Determining bot user ID")
		users, err := slackAPI.GetUsers()
		if err != nil {
			log.Fatal(err)
		}

		for _, user := range users {
			if !user.IsBot {
				continue
			}

			if user.Name == botName {
				botID = user.ID
				break
			}
		}
		if botID == "" {
			log.Fatal("could not find bot in the list of names, check if the bot is called \"" + botName + "\" ")
		}
	}(wg)

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		log.Println("Determining channels ID")
		publicChannels, err := slackAPI.GetChannels(false)
		if err != nil {
			log.Fatal(err)
		}

		for _, channel := range publicChannels {
			if chn, ok := channels[channel.Name]; ok {
				chn.slackID = "#" + channel.ID
				channels[channel.Name] = chn
			}
		}
	}(wg)

	wg.Wait()
	log.Printf("Initialized %s with ID: %s\n", botName, botID)
}

func main() {
	rtm := slackAPI.NewRTM()
	go rtm.ManageConnection()

	for {
		select {
		case msg := <-rtm.IncomingEvents:
			switch message := msg.Data.(type) {
			case *slack.MessageEvent:
				go handleMessage(message)

			case *slack.TeamJoinEvent:
				go teamJoined(message)
			default:
			}
		}
	}
}

func teamJoined(event *slack.TeamJoinEvent) {
	message := `Hello ` + event.User.Name + `,


Welcome to the Gophers Slack channel.

This Slack is meant to connect gophers from all over the world in a central place.

We have a few rules that you can see here: http://coc.golangbridge.org
There is also a forum: https://forum.golangbridge.org

Here's a list of a few channels you could join:
`

	for idx := range channels {
		message += `<` + channels[idx].slackID + `|` + idx + `> -> ` + channels[idx].description + "\n"
	}

	message += `
There are quite a few other channels, depending on your interests or location (we have city / country wide channels).
Just click on the channel list and search for anything that crosses your mind.

To share code, you should use: https://play.golang.org/ as it makes it easy for others to help you.

Final thing, #general might be too chatty at times but don't be shy to ask your Go related question.


Now enjoy your stay and have fun.`

	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.User.ID, message, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func handleMessage(event *slack.MessageEvent) {
	if event.BotID != "" || event.User == "" || event.SubType == "bot_message" {
		return
	}

	eventText := strings.ToLower(event.Text)

	if devMode == "true" {
		log.Printf("got message: %s\n", eventText)
	}

	if strings.Contains(eventText, "newbie resources") {
		newbieResources(event)
		return
	}

	// TODO should we check for ``` or messages of a certain length?
	if !strings.Contains(eventText, "nolink") &&
		event.File != nil &&
		(event.File.Filetype == "go" || event.File.Filetype == "text") {
		suggestPlayground(event)
		return
	}

	// All the variations of table flip seem to include this characters so... potato?
	if strings.Contains(eventText, "︵") || strings.Contains(eventText, "彡") {
		tableUnflip(event)
		return
	}

	if strings.Contains(eventText, "oss help") {
		ossHelp(event)
		return
	}

	if strings.Contains(eventText, "go forks") {
		goForks(event)
		return
	}

	if strings.Contains(eventText, "blog about http timeouts") {
		dealWithHTTPTimeouts(event)
		return
	}

	if strings.HasPrefix(eventText, "ghd/") {
		godoc(event, "github.com/", 4)
		return
	}

	if strings.HasPrefix(eventText, "d/") {
		godoc(event, "", 2)
		return
	}

	if strings.Contains(eventText, strings.ToLower(botName)) || strings.Contains(eventText, strings.ToLower(botID)) {
		if strings.Contains(eventText, "library for") ||
			strings.Contains(eventText, "library in go for") ||
			strings.Contains(eventText, "go library for") {
			searchLibrary(event)
			return
		}

		if strings.Contains(eventText, "thank") ||
			strings.Contains(eventText, "cheers") ||
			strings.Contains(eventText, "hello") ||
			strings.Contains(eventText, "hi") {
			reactToEvent(event, "gopher")
			return
		}

		if strings.Contains(eventText, "wave") {
			reactToEvent(event, "wave")
			reactToEvent(event, "gopher")
			return
		}

		if strings.Contains(eventText, "version") {
			replyVersion(event)
		}
		return
	}
}

func newbieResources(event *slack.MessageEvent) {
	newbieResources := slack.Attachment{
		Text: `First you should take the language tour: <http://tour.golang.org/>

Then, you should visit:
 - <https://golang.org/doc/code.html> To learn how to organize your Go workspace
 - <https://golang.org/doc/effective_go.html> which would help you be more effective at writing Go
 - <https://golang.org/ref/spec> will help you learn more about the language itself
 - <https://golang.org/doc/#articles> For a lot more reading material

There are some awesome websites as well:
 - <https://blog.gopheracademy.com> Well great resources for Gophers in general
 - <http://gotime.fm> For a weekly podcast of Go awesomeness
 - <https://gobyexample.com> If you are looking for examples of how to do things in Go
 - <http://go-database-sql.org> If you are looking for how to use SQL databases in Go
 - <http://gophervids.appspot.com> For a list of Go related videos from various authors

Finally, <https://github.com/golang/go/wiki#learning-more-about-go> will give a list of more resources to learn Go`,
	}

	params := slack.PostMessageParameters{}
	params.Attachments = []slack.Attachment{newbieResources}
	_, _, err := slackAPI.PostMessage(event.Channel, "Here are some resources you might want to check if you are new to Go:", params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func suggestPlayground(event *slack.MessageEvent) {
	if event.File == nil {
		return
	}

	info, _, _, err := slackAPI.GetFileInfo(event.File.ID, 0, 0)
	if err != nil {
		log.Printf("error while getting file info: %v", err)
		return
	}

	c := &http.Client{
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

	req, err := http.NewRequest("GET", info.URLPrivateDownload, nil)
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req.Header.Add("Authorization", "Bearer "+slackToken)
	resp, err := c.Do(req)
	if err != nil {
		log.Printf("error while fetching the file %v\n", err)
		return
	}

	file, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Printf("error while reading the file %v\n", err)
		return
	}

	requestBody := bytes.NewBuffer(file)

	req, err = http.NewRequest("POST", "https://play.golang.org/share", requestBody)
	if err != nil {
		log.Printf("failed to get playground link: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req.Header.Add("Content-Length", strconv.Itoa(len(file)))

	resp, err = c.Do(req)
	if err != nil {
		log.Printf("failed to get playground link: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("got non-200 response: %v", resp.StatusCode)
		return
	}

	linkID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("failed to get playground link: %v", err)
		return
	}

	params := slack.PostMessageParameters{}
	_, _, err = slackAPI.PostMessage(event.Channel, `I've uploaded this file to the Go Playground to allow easier collaboration: <https://play.golang.org/p/`+string(linkID)+`>`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}

	_, _, err = slackAPI.PostMessage(event.User, `Hello. I've noticed you uploaded a Go file. To enable collaboration and make this easier to get help, please consider using: <https://play.golang.org>. Thank you.`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func ossHelp(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.Channel, `Here's a list of projects which could need some help from contributors like you: <https://github.com/corylanou/oss-helpwanted>`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func goForks(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.Channel, `Here's a blog post which will help you to work with forks for Go libraries: <http://blog.sgmansfield.com/2016/06/working-with-forks-in-go/>`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func dealWithHTTPTimeouts(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.Channel, `Here's a blog post which will help with http timeouts in Go: <https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/>`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func tableUnflip(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.Channel, `┬─┬ノ( º _ ºノ)`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func searchLibrary(event *slack.MessageEvent) {
	searchTerm := strings.ToLower(event.Text)
	if idx := strings.Index(searchTerm, "library for"); idx != -1 {
		searchTerm = event.Text[idx+11:]
	} else if idx := strings.Index(searchTerm, "library in go for"); idx != -1 {
		searchTerm = event.Text[idx+17:]
	} else if idx := strings.Index(searchTerm, "go library for"); idx != -1 {
		searchTerm = event.Text[idx+14:]
	}

	searchTerm = slackLinkRE.ReplaceAllString(searchTerm, "")
	searchTerm = emojiRE.ReplaceAllString(searchTerm, "")

	if idx := strings.Index(searchTerm, "in go"); idx != -1 {
		searchTerm = searchTerm[:idx] + searchTerm[idx+5:]
	}

	searchTerm = strings.Trim(searchTerm, "?;., ")
	if len(searchTerm) == 0 || len(searchTerm) > 100 {
		return
	}
	searchTerm = url.QueryEscape(searchTerm)
	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.Channel, `You can try to look here: <https://godoc.org/?q=`+searchTerm+`> or here <http://go-search.org/search?q=`+searchTerm+`>`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func godoc(event *slack.MessageEvent, prefix string, position int) {
	link := event.Text[position:]
	if strings.Contains(link, " ") {
		link = link[:strings.Index(link, " ")]
	}

	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.Channel, `<https://godoc.org/`+prefix+link+`>`, params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func reactToEvent(event *slack.MessageEvent, reaction string) {
	item := slack.ItemRef{
		Channel:   event.Channel,
		Timestamp: event.Timestamp,
	}
	err := slackAPI.AddReaction(reaction, item)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}

func replyVersion(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{}
	_, _, err := slackAPI.PostMessage(event.User, fmt.Sprintf("My version is: %s", botVersion), params)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
}
