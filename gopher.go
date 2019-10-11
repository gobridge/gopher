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
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gobridge/gopher/bot"
	"github.com/gobridge/gopher/gerrit"
	"github.com/gobridge/gopher/gotime"
	"github.com/gobridge/gopher/handlers"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/trace"
	"github.com/nlopes/slack"
	"google.golang.org/api/option"
)

const defaultCredentialFile = "/tmp/trace/trace.json" // Also /tmp/datastore/datastore.json :-(

var BotVersion = "HEAD"

func main() {
	rand.Seed(time.Now().UnixNano())

	log.SetFlags(log.Lshortfile)
	logf := log.Printf

	var (
		slackBotToken     = os.Getenv("GOPHERS_SLACK_BOT_TOKEN")
		googleCredentials = os.Getenv("GOOGLE_CREDENTIALS")
		googleProjectID   = os.Getenv("GOOGLE_PROJECT_ID")
		opsChannel        = os.Getenv("OPS_CHANNEL")
		devMode           = os.Getenv("GOPHERS_SLACK_BOT_DEV_MODE") == "true"
	)

	if slackBotToken == "" {
		log.Fatalln("slack bot token must be set in GOPHERS_SLACK_BOT_TOKEN")
	}

	if googleCredentials == "" {
		// FIXME: This doesn't deal with the default credentials in per service locations.

		if _, err := os.Stat(defaultCredentialFile); err == nil {
			googleCredentials = defaultCredentialFile
		}
	} else {
		googleCredentials = decodeGoogleCredentialsToFile(googleCredentials)
	}

	ctx := context.Background()

	traceClient, err := trace.NewClient(ctx, googleProjectID, option.WithServiceAccountFile(googleCredentials))
	if err != nil {
		log.Fatalln("Unable to create trace client:", err)
	}
	span := traceClient.NewSpan("main")
	ctx = trace.NewContext(ctx, span)

	dsClient, err := datastore.NewClient(ctx, googleProjectID, option.WithServiceAccountFile(googleCredentials))
	if err != nil {
		log.Fatalln("Unable to create datastore client:", err)
	}
	defer dsClient.Close()

	traceHTTPClient := &http.Client{
		Transport: trace.Transport{
			Base: &http.Transport{
				Dial: (&net.Dialer{
					Timeout:   15 * time.Second,
					KeepAlive: 30 * time.Second,
				}).Dial,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
	slackBotAPI := slack.New(slackBotToken,
		slack.OptionHTTPClient(traceHTTPClient),
	)

	welcomeChannels := []handlers.Channel{
		{Name: "general", Description: "for general Go questions or help"},
		{Name: "newbies", Description: "for newbie resources"},
		{Name: "reviews", Description: "for code reviews"},
		{Name: "gotimefm", Description: "for the awesome live podcast"},
		{Name: "remotemeetup", Description: "for remote meetup"},
		{Name: "showandtell", Description: "for telling the world about the thing you are working on"},
		{Name: "jobs", Description: "for jobs related to Go"},
	}
	recommendedChannels := append(welcomeChannels, []handlers.Channel{
		{Name: "performance", Description: "anything and everything performance related"},
		{Name: "devops", Description: "for devops related discussions"},
		{Name: "security", Description: "for security related discussions"},
		{Name: "aws", Description: "if you are interested in AWS"},
		{Name: "goreviews", Description: "talk to the Go team about a certain CL"},
		{Name: "golang-cls", Description: "get real time udates from the merged CL for Go itself"},
		{Name: "bbq", Description: "Go controlling your bbq grill? Yes, we have that"},
	}...)

	joinHandler := handlers.Join(welcomeChannels)

	msgHandlers := handlers.ProcessLinear(
		handlers.ReactWhenContains("my adorable little gophers", "gopher"),
		handlers.ReactWhenContains("bbq", "bbqgopher"),
		handlers.ReactWhenContains("buffalo", "gobuffalo"),
		handlers.ReactWhenContains("gobuffalo", "gobuffalo"),
		handlers.ReactWhenContains("ghost", "ghost"),
		handlers.ReactWhenContains("ermergerd", "dragon"),
		handlers.ReactWhenContains("ermahgerd", "dragon"),
		handlers.ReactWhenContains("dragon", "dragon"),
		handlers.ReactWhenContains("spacex", "rocket"),
		handlers.ReactWhenContains("beer me", "beer", "beers"),
		handlers.ReactWhenContains("spacemacs", "spacemacs"),
		handlers.ReactWhenContainsRand("emacs", "vim"),
		handlers.ReactWhenContainsRand("vim", "emacs"),
		handlers.RespondWhenContains("︵", "┬─┬ノ( º _ ºノ)"),
		handlers.RespondWhenContains("彡", "┬─┬ノ( º _ ºノ)"),

		handlers.Songs(), // TODO: Is this used?
		handlers.SuggestPlayground(traceHTTPClient, slackBotAPI, logf, 10),
		handlers.LinkToGoDoc("d/", "https://godoc.org/"),
		handlers.LinkToGoDoc("ghd/", "https://godoc.org/github.com/"),

		handlers.WhenDirectedToBot(handlers.ProcessLinear(
			handlers.ReactWhenContains("thank", "gopher"),
			handlers.ReactWhenContains("cheers", "gopher"),
			handlers.ReactWhenContains("hello", "gopher"),
			handlers.ReactWhenHasPrefix("wave", "wave", "gopher"),
			handlers.BotStack([]string{"stack", "where do you live?"}),
			handlers.BotVersion("version", BotVersion),
			handlers.CoinFlip([]string{"coin flip", "flip a coin"}),
			handlers.RecommendedChannels("recommended channels", recommendedChannels),
			handlers.NewbieResources("newbie resources"),
			handlers.SearchForLibrary("library for"),
			handlers.XKCD("xkcd:",
				map[string]int{
					"standards":    927,
					"compiling":    303,
					"optimization": 1691,
				},
				logf,
			),

			handlers.RespondTo([]string{"recommended", "recommended blogs"},
				strings.Join([]string{
					`Here are some popular blog posts and Twitter accounts you should follow:`,
					`- Peter Bourgon <https://twitter.com/peterbourgon|@peterbourgon> - <https://peter.bourgon.org/blog>`,
					`- Carlisia Campos <https://twitter.com/carlisia|@carlisia>`,
					`- Dave Cheney <https://twitter.com/davecheney|@davecheney> - <http://dave.cheney.net>`,
					`- Jaana Burcu Dogan <https://twitter.com/rakyll|@rakyll> - <http://golang.rakyll.org>`,
					`- Jessie Frazelle <https://twitter.com/jessfraz|@jessfraz> - <https://blog.jessfraz.com>`,
					`- William "Bill" Kennedy <https://twitter.com|@goinggodotnet> - <https://www.goinggo.net>`,
					`- Brian Ketelsen <https://twitter.com/bketelsen|@bketelsen> - <https://www.brianketelsen.com/blog>`,
				}, "\n"),
			),
			handlers.RespondTo([]string{"books"},
				strings.Join([]string{
					`Here are some popular books you can use to get started:`,
					`- William Kennedy, Brian Ketelsen, Erik St. Martin Go In Action <https://www.manning.com/books/go-in-action>`,
					`- Alan A A Donovan, Brian W Kernighan The Go Programming Language <https://www.gopl.io>`,
					`- Mat Ryer Go Programming Blueprints 2nd Edition <https://www.packtpub.com/application-development/go-programming-blueprints-second-edition>`,
				}, "\n"),
			),
			handlers.RespondTo([]string{"oss help", "oss help wanted"},
				`Here's a list of projects which could need some help from contributors like you: <https://github.com/corylanou/oss-helpwanted>`,
			),
			handlers.RespondTo([]string{"work with forks", "working with forks"},
				`Here's how to work with package forks in Go: <http://blog.sgmansfield.com/2016/06/working-with-forks-in-go/>`,
			),
			handlers.RespondTo([]string{"block forever", "how to block forever"},
				`Here's how to block forever in Go: <http://blog.sgmansfield.com/2016/06/how-to-block-forever-in-go/>`,
			),
			handlers.RespondTo([]string{"http timeouts"},
				`Here's a blog post which will help with http timeouts in Go: <https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/>`,
			),
			handlers.RespondTo([]string{"slices", "slice internals"},
				strings.Join([]string{
					`The following posts will explain how slices, maps and strings work in Go:`,
					`- <https://blog.golang.org/go-slices-usage-and-internals>`,
					`- <https://blog.golang.org/slices>`,
					`- <https://blog.golang.org/strings>`,
				}, "\n"),
			),
			handlers.RespondTo([]string{"databases", "database tutorial"},
				`Here's how to work with database/sql in Go: <http://go-database-sql.org/>`,
			),
			handlers.RespondTo(
				[]string{
					"project layout",
					"package layout",
					"project structure",
					"package structure",
				},
				strings.Join([]string{
					`These articles will explain how to organize your Go packages:`,
					`- <https://rakyll.org/style-packages/>`,
					`- <https://medium.com/@benbjohnson/standard-package-layout-7cdbc8391fc1#.ds38va3pp>`,
					`- <https://peter.bourgon.org/go-best-practices-2016/#repository-structure>`,
					``,
					`This article will help you understand the design philosophy for packages: <https://www.goinggo.net/2017/02/design-philosophy-on-packaging.html>`,
				}, "\n"),
			),
			handlers.RespondTo([]string{"idiomatic go"},
				`Tips on how to write idiomatic Go code <https://dmitri.shuralyov.com/idiomatic-go>`,
			),
			handlers.RespondTo([]string{"gotchas", "avoid gotchas"},
				`Read this article if you want to understand and avoid common gotchas in Go <https://divan.github.io/posts/avoid_gotchas>`,
			),
			handlers.RespondTo([]string{"style", "style guide"},
				`Here is the Go style guide by Uber: <https://github.com/uber-go/guide/blob/master/style.md>`,
			),
			handlers.RespondTo([]string{"source", "source code"},
				`My source code is here <https://github.com/gobridge/gopher>`,
			),
			handlers.RespondTo([]string{"di", "dependency injection"},
				strings.Join([]string{
					`If you'd like to learn more about how to use Dependency Injection in Go, please review this post:`,
					`- <https://appliedgo.net/di/>`,
				}, "\n"),
			),
			handlers.RespondTo([]string{"pointer performance"},
				strings.Join([]string{
					`The answer to whether using a pointer offers a performance gain is complex and is not always the case. Please read these posts for more information:`,
					`- <https://medium.com/@vCabbage/go-are-pointers-a-performance-optimization-a95840d3ef85>`,
					`- <https://segment.com/blog/allocation-efficiency-in-high-performance-go-services/>`,
				}, "\n"),
			),
			handlers.RespondTo([]string{"help"},
				strings.Join([]string{
					`Here's a list of supported commands`,
					"- `newbie resources` -> get a list of newbie resources",
					"- `newbie resources pvt` -> get a list of newbie resources as a private message",
					"- `recommended channels` -> get a list of recommended channels",
					"- `oss help` -> help the open-source community",
					"- `work with forks` -> how to work with forks of packages",
					"- `idiomatic go` -> learn how to write more idiomatic Go code",
					"- `block forever` -> how to block forever",
					"- `http timeouts` -> tutorial about dealing with timeouts and http",
					"- `database tutorial` -> tutorial about using sql databases",
					"- `package layout` -> learn how to structure your Go package",
					"- `avoid gotchas` -> avoid common gotchas in Go",
					"- `library for <name>` -> search a go package that matches <name>",
					"- `flip a coin` -> flip a coin",
					"- `source code` -> location of my source code",
					"- `where do you live?` OR `stack` -> get information about where the tech stack behind @gopher",
				}, "\n"),
			),
			handlers.RespondTo(
				[]string{
					"gopath",
					"gopath problem",
					"issue with gopath",
					"help with gopath",
				},
				strings.Join([]string{
					"Your project should be structured as follows:",
					"```GOPATH=~/go",
					"~/go/src/sourcecontrol/username/project/```",
					"Whilst you _can_ get around the GOPATH, it's ill-advised. Read more about the GOPATH here: https://github.com/golang/go/wiki/GOPATH",
				}, "\n"),
			),
		)),
	)

	b := bot.New(slackBotAPI, traceClient, devMode, logf, msgHandlers, joinHandler)
	err = b.Init(ctx)
	if err != nil {
		log.Fatalln("Unable to init bot:", err)
	}
	if opsChannel != "" {
		cs := span.NewChild("main.AnnouncingStartupFinish")
		err = b.PostMessage(ctx, opsChannel, `Deployed version: `+BotVersion)
		cs.Finish()
		if err != nil {
			logf(`failed to deploy version: %s`, BotVersion)
		}
	}

	// Gerrit CL Notifications
	if !devMode {
		notify := func(cl gerrit.GerritCL) bool {
			msg := fmt.Sprintf("[%d] %s: %s", cl.Number, cl.Message(), cl.Link())
			err = b.PostMessage(ctx, "golang-cls", msg,
				slack.MsgOptionAttachments(slack.Attachment{
					Title:     cl.Subject,
					TitleLink: cl.Link(),
					Text:      cl.Revisions[cl.CurrentRevision].Commit.Message,
					Footer:    cl.ChangeID,
				}),
			)
			if err != nil {
				logf("error posting to #golang-cls: %v", err)
				return false
			}
			return true
		}

		dsClient, err := datastore.NewClient(ctx, googleProjectID, option.WithServiceAccountFile(googleCredentials))
		if err != nil {
			log.Fatalln("Unable to create datastore client:", err)
		}
		defer dsClient.Close()

		store := gerrit.NewGCPStore(dsClient)

		g, err := gerrit.New(ctx, store, traceHTTPClient, logf, notify)
		if err != nil {
			log.Fatalln("Unable to initialize gerrit poller:", err)
		}

		go func() {
			ticker := time.NewTicker(30 * time.Minute)

			g.Poll(ctx)
			for range ticker.C {
				g.Poll(ctx)
			}
		}()
	} else {
		logf("gerrit updates disabled in devMode")
	}

	// GoTime Livestream Notifications
	{
		notify := func() bool {
			err = b.PostMessage(ctx, "gotimefm", ":tada: GoTimeFM is now live :tada:")
			if err != nil {
				logf("error posting to #gotimefm: %v", err)
				return false
			}
			return true
		}

		gt := gotime.New(traceHTTPClient, 30*time.Minute, notify)
		go func() {
			gotimefm := time.NewTicker(1 * time.Minute)
			defer gotimefm.Stop()

			for range gotimefm.C {
				err := gt.Poll(ctx)
				if err != nil {
					logf("polling GoTime: %v", err)
				}
			}
		}()
	}

	// healthz endpoint
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.NotFound(w, r)
				return
			}

			span := traceClient.SpanFromRequest(r)
			defer span.Finish()

			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, `{"version": "`+BotVersion+`"}`)
		})

		port := os.Getenv("PORT")
		if port == "" {
			port = "8081"
		}

		s := http.Server{
			Addr:         ":" + port,
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		log.Fatal(s.ListenAndServe())
	}()

	log.Println("Gopher is now running")
	span.Finish()
	select {}
}

// decode the base64 encoded google credential file data to a temporary file on the file system.
// This allows credential information to be placed into a single config var like so:
// export GOOGLE_CREDENTIALS="$(base64 ./path/to/credential/file.json)"
// or
// heroku config:set GOOGLE_CREDENTIALS="$(base64 ./path/to/credential/file.json)"
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
