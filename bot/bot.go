package bot

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/trace"
	"github.com/ChimeraCoder/anaconda"
	"github.com/nlopes/slack"
)

type (
	slackChan struct {
		description string
		slackID     string
		welcome     bool
		special     bool
	}

	// Client is the HTTP client
	Client interface {
		Do(r *http.Request) (*http.Response, error)
	}

	// Logger function
	Logger func(message string, args ...interface{})

	// Bot structure
	Bot struct {
		id          string
		msgprefix   string
		gerritLink  string
		name        string
		token       string
		version     string
		users       map[string]string
		client      Client
		devMode     bool
		emojiRE     *regexp.Regexp
		slackLinkRE *regexp.Regexp
		channels    map[string]slackChan
		slackBotAPI *slack.Client
		twitterAPI  *anaconda.TwitterApi
		logf        Logger
		dsClient    *datastore.Client
		traceClient *trace.Client

		goTimeLastNotified time.Time
	}

	slackHandler func(context.Context, *Bot, *slack.MessageEvent)
)

var welcomeMessage = ""

// Init must be called before anything else in order to initialize the bot
func (b *Bot) Init(ctx context.Context, rtm *slack.RTM, span *trace.Span) error {
	initSpan := span.NewChild("b.Init")
	defer initSpan.Finish()

	b.logf("Determining bot / user IDs")

	b.id = "U1XK0CWSZ"
	b.users = map[string]string{
		"dlsniper": "U03L9MPTE",
	}

	b.msgprefix = strings.ToLower("<@" + b.id + ">")

	b.logf("Determining channels ID\n")
	childSpan := initSpan.NewChild("slackApi.GetChannels")
	publicChannels, err := b.slackBotAPI.GetChannelsContext(ctx, true)
	childSpan.Finish()
	if err != nil {
		return err
	}

	for _, channel := range publicChannels {
		channelName := strings.ToLower(channel.Name)
		if chn, ok := b.channels[channelName]; ok {
			chn.slackID = "#" + channel.ID
			b.channels[channelName] = chn
		}
	}

	publicChannels = nil

	b.logf("Determining groups ID\n")
	childSpan = initSpan.NewChild("slackApi.GetGroups")
	botGroups, err := b.slackBotAPI.GetGroupsContext(ctx, true)
	childSpan.Finish()
	for _, group := range botGroups {
		groupName := strings.ToLower(group.Name)
		if chn, ok := b.channels[groupName]; ok && b.channels[groupName].slackID == "" {
			chn.slackID = group.ID
			b.channels[groupName] = chn
		}
	}

	botGroups = nil

	b.logf("Initialized %s with ID: %s\n", b.name, b.id)
	params := slack.PostMessageParameters{AsUser: true}
	childSpan = initSpan.NewChild("b.AnnouncingStartupFinish")
	_, _, err = b.slackBotAPI.PostMessageContext(ctx, b.users["dlsniper"], fmt.Sprintf(`Deployed version: %s`, b.version), params)
	childSpan.Finish()

	if err != nil {
		b.logf(`failed to deploy version: %s`, b.version)
	}

	welcomeMessage = `,


Welcome to the Gophers Slack channel.
This Slack is meant to connect gophers from all over the world in a central place.
There is also a forum: https://forum.golangbridge.org, you might want to check it out as well.
We have a few rules that you can see here: http://coc.golangbridge.org.

Here's a list of a few channels you could join:
`

	for idx, val := range b.channels {
		if !val.welcome {
			continue
		}
		welcomeMessage += `<` + val.slackID + `|` + idx + `> -> ` + val.description + "\n"
	}

	welcomeMessage += `

If you want more suggestions, type "recommended channels".
There are quite a few other channels, depending on your interests or location (we have city / country wide channels).
Just click on the channel list and search for anything that crosses your mind.

To share code, you should use: https://play.golang.org/ as it makes it easy for others to help you.

If you are new to Go and want a copy of the Go In Action book, https://www.manning.com/books/go-in-action, please send an email to @wkennedy at bill@ardanlabs.com

If you are interested in a free copy of the Go Web Programming book by Sau Sheong Chang, @sausheong, please send him an email at sausheong@gmail.com

In case you want to customize your profile picture, you can use https://gopherize.me/ to create a custom gopher.

Final thing, #general might be too chatty at times but don't be shy to ask your Go related question.


Now, enjoy the community and have fun.`

	return err
}

// TeamJoined is called when the someone joins the team
func (b *Bot) TeamJoined(event *slack.TeamJoinEvent) {
	span := b.traceClient.NewSpan("b.TeamJoined")
	defer span.Finish()

	if b.devMode {
		return
	}

	message := `Hello ` + event.User.Name + welcomeMessage

	params := slack.PostMessageParameters{AsUser: true, LinkNames: 1}
	ctx := trace.NewContext(context.Background(), span)
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.User.ID, message, params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func (b *Bot) isBotMessage(event *slack.MessageEvent, eventText string) bool {
	prefixes := []string{
		b.msgprefix,
		"gopher",
	}

	for _, p := range prefixes {
		if strings.HasPrefix(eventText, p) {
			return true
		}
	}

	// Direct message channels always starts with 'D'
	return strings.HasPrefix(event.Channel, "D")
}

func (b *Bot) trimBot(msg string) string {
	msg = strings.Replace(msg, strings.ToLower(b.msgprefix), "", 1)
	msg = strings.TrimPrefix(msg, "gopher")
	msg = strings.Trim(msg, " :\n")

	return msg
}

// limit access to certain functionality
func (b *Bot) specialRestrictions(restriction string, event *slack.MessageEvent) bool {
	if restriction == "golang_cls" {
		return event.Channel == b.channels["golang_cls"].slackID
	}

	return false
}

var (
	// Generic responses to all messages
	containsToReactions = map[string][]string{
		"︵": {"┬─┬ノ( º _ ºノ)"},
		"彡": {"┬─┬ノ( º _ ºノ)"},
		"my adorable little gophers": {"gopher"},
		"bbq":       {"bbqgopher"},
		"buffalo":   {"gobuffalo"},
		"gobuffalo": {"gobuffalo"},
		"ghost":     {"ghost"},
		"ermergerd": {"dragon"},
		"ermahgerd": {"dragon"},
		"dragon":    {"dragon"},
		"spacex":    {"rocket"},
		"beer me":   {"beer", "beers"},
	}

	reactWithMessage = map[string]struct{}{
		"︵": {},
		"彡": {},
	}

	// Bot-directed message reactions / responses from here down
	botEventTextToFunc = map[string]slackHandler{
		"newbie resources":     newbieResourcesPublic,
		"newbie resources pvt": newbieResourcesPrivate,
		"recommended channels": recommendedChannels,
		"flip coin":            flipCoin,
		"flip a coin":          flipCoin,
		"version":              botVersion,
	}

	botEventTextToResponse = map[string][]string{
		"recommended blogs": {
			`Here are some popular blog posts and Twitter accounts you should follow:`,
			`- Peter Bourgon <https://twitter.com/peterbourgon|@peterbourgon> - <https://peter.bourgon.org/blog>`,
			`- Carlisia Campos <https://twitter.com/carlisia|@carlisia>`,
			`- Dave Cheney <https://twitter.com/davecheney|@davecheney> - <http://dave.cheney.net>`,
			`- Jaana Burcu Dogan <https://twitter.com/rakyll|@rakyll> - <http://golang.rakyll.org>`,
			`- Jessie Frazelle <https://twitter.com/jessfraz|@jessfraz> - <https://blog.jessfraz.com>`,
			`- William "Bill" Kennedy <https://twitter.com|@goinggodotnet> - <https://www.goinggo.net>`,
			`- Brian Ketelsen <https://twitter.com/bketelsen|@bketelsen> - <https://www.brianketelsen.com/blog>`,
		},
		"oss help wanted": {
			`Here's a list of projects which could need some help from contributors like you: <https://github.com/corylanou/oss-helpwanted>`,
		},
		"working with forks": {
			`Here's how to work with package forks in Go: <http://blog.sgmansfield.com/2016/06/working-with-forks-in-go/>`,
		},
		"block forever": {
			`Here's how to block forever in Go: <http://blog.sgmansfield.com/2016/06/how-to-block-forever-in-go/>`,
		},
		"http timeouts": {
			`Here's a blog post which will help with http timeouts in Go: <https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/>`,
		},
		"slices": {
			`The following posts will explain how slices, maps and strings work in Go:`,
			`- <https://blog.golang.org/slices>`,
			`- <https://blog.golang.org/go-slices-usage-and-internals>`,
			`- <https://blog.golang.org/strings>`,
		},
		"database tutorial": {
			`Here's how to work with database/sql in Go: <http://go-database-sql.org/>`,
		},
		"package layout": {
			`These articles will explain how to organize your Go packages:`,
			`- <https://rakyll.org/style-packages/>`,
			`- <https://medium.com/@benbjohnson/standard-package-layout-7cdbc8391fc1#.ds38va3pp>`,
			`- <https://peter.bourgon.org/go-best-practices-2016/#repository-structure>`,
			``,
			`This article will help you understand the design philosophy for packages: <https://www.goinggo.net/2017/02/design-philosophy-on-packaging.html>`,
		},
		"idiomatic go": {
			`Tips on how to write idiomatic Go code <https://dmitri.shuralyov.com/idiomatic-go>`,
		},
		"avoid gotchas": {
			`Read this article if you want to understand and avoid common gotchas in Go <https://divan.github.io/posts/avoid_gotchas>`,
		},
		"source code": {
			`My source code is here <https://github.com/gopheracademy/gopher>`,
		},
		"stack": {
			`I'm currently living in the Clouds, powered by Google Container Engine (GKE) <https://cloud.google.com/container-engine>.`,
			`I find my way to home using CircleCI <https://circleci.com> and Kubernetes (k8s) <http://kubernetes.io>.`,
			`You can find my heart at: <https://github.com/gopheracademy/gopher>.`,
		},
		"help": {
			`Here's a list of supported commands`,
			`- "newbie resources" -> get a list of newbie resources`,
			`- "newbie resources pvt" -> get a list of newbie resources as a private message`,
			`- "recommended channels" -> get a list of recommended channels`,
			`- "oss help" -> help the open-source community`,
			`- "work with forks" -> how to work with forks of packages`,
			`- "idiomatic go" -> learn how to write more idiomatic Go code`,
			`- "block forever" -> how to block forever`,
			`- "http timeouts" -> tutorial about dealing with timeouts and http`,
			`- "database tutorial" -> tutorial about using sql databases`,
			`- "package layout" -> learn how to structure your Go package`,
			`- "avoid gotchas" -> avoid common gotchas in Go`,
			`- "library for <name>" -> search a go package that matches <name>`,
			`- "flip a coin" -> flip a coin`,
			`- "source code" -> location of my source code`,
			`- "where do you live?" OR "stack" -> get information about where the tech stack behind @gopher`,
		},
	}

	botEventTextToResponseAliases = map[string]string{
		"recommended":          "recommended blogs",
		"oss help":             "oss help wanted",
		"work with forks":      "working with forks",
		"how to block forever": "block forever",
		"slice internals":      "slices",
		"databases":            "database tutorial",
		"gotchas":              "avoid gotchas",
		"source":               "source code",
		"where do you live?":   "stack",
		"package structure":    "package layout",
		"project structure":    "package layout",
		"project layout":       "package layout",
	}

	botPrefixToFunc = map[string]slackHandler{
		"xkcd:":       xkcd,
		"library for": searchLibrary,
		"share cl":    shareCL,
	}

	botContainsToReactions = map[string][]string{
		"thank":  {"gopher"},
		"cheers": {"gopher"},
		"hello":  {"gopher"},
	}

	botHasPrefixToReactions = map[string][]string{
		"wave": {"wave", "gopher"},
	}
)

// HandleMessage will process the incoming message and repspond appropriately
func (b *Bot) HandleMessage(event *slack.MessageEvent) {
	if event.BotID != "" || event.User == "" || event.SubType == "bot_message" {
		return
	}

	eventText := strings.Trim(strings.ToLower(event.Text), " \n\r")

	if b.devMode {
		b.logf("%#v\n", *event)
		b.logf("got message: %s\n", eventText)
		b.logf("isBotMessage: %t\n", b.isBotMessage(event, eventText))
		b.logf("channel: %s -> message: %q\n", event.Channel, b.trimBot(eventText))
		return
	}

	span := b.traceClient.NewSpan("b.HandleMessage")
	span.SetLabel("eventText", eventText)
	defer span.Finish()

	ctx := trace.NewContext(context.Background(), span)

	// On messages containing song links, share alternate music platforms in thread
	songLinkHandler(ctx, b, event)

	// Reactions to all messages (including those not directed at the bot)
	// that contain a certain string
	for needle, reactions := range containsToReactions {
		if strings.Contains(eventText, needle) {
			if _, ok := reactWithMessage[needle]; ok {
				for _, reaction := range reactions {
					respond(ctx, b, event, reaction)
				}
			} else {
				for _, reaction := range reactions {
					b.reactToEvent(ctx, event, reaction)
				}
			}
			return
		}
	}

	if strings.HasPrefix(eventText, "ghd/") {
		b.godoc(ctx, event, "github.com/", 4)
		return
	}

	if strings.HasPrefix(eventText, "d/") {
		b.godoc(ctx, event, "", 2)
		return
	}

	if !strings.Contains(eventText, "nolink") &&
		event.File != nil &&
		(event.File.Filetype == "go" || event.File.Filetype == "text") {
		b.suggestPlayground(ctx, event)
		return
	}

	// We assume that the user actually wanted to have a code snippet shared
	if !strings.HasPrefix(eventText, "nolink") &&
		strings.Count(eventText, "\n") > 9 {
		b.suggestPlayground2(ctx, event)
		return
	}

	// All messages past this point are directed to @gopher itself
	if !b.isBotMessage(event, eventText) {
		return
	}

	eventText = b.trimBot(eventText)
	if b.devMode {
		b.logf("message: %q\n", eventText)
	}

	// Responses that need some logic behind them
	if responseFunc, ok := botEventTextToFunc[eventText]; ok {
		responseFunc(ctx, b, event)
		return
	}

	// Responses that are just a canned string response
	if responseLines, ok := botEventTextToResponse[eventText]; ok {
		response := strings.Join(responseLines, "\n")
		respond(ctx, b, event, response)
		return
	}

	// aliases for the above canned responses
	if key, ok := botEventTextToResponseAliases[eventText]; ok {
		if responseLines, ok := botEventTextToResponse[key]; ok {
			response := strings.Join(responseLines, "\n")
			respond(ctx, b, event, response)
			return
		}

		b.logf("Bad response alias: %v", eventText)
		return
	}

	// Reacting based on if the message contains a needle
	for needle, reactions := range botContainsToReactions {
		if strings.Contains(eventText, needle) {
			for _, reaction := range reactions {
				b.reactToEvent(ctx, event, reaction)
			}
			return
		}
	}

	// Reacting based on a prefix of the message
	for prefix, reactions := range botHasPrefixToReactions {
		if strings.HasPrefix(eventText, prefix) {
			for _, reaction := range reactions {
				b.reactToEvent(ctx, event, reaction)
			}
			return
		}
	}

	// More responses that need some logic behind them
	for prefix, responseFunc := range botPrefixToFunc {
		if strings.HasPrefix(eventText, prefix) {
			responseFunc(ctx, b, event)
			return
		}
	}
}

func newbieResourcesPublic(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	newbieResources(ctx, b, event, false)
}

func newbieResourcesPrivate(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	newbieResources(ctx, b, event, true)
}

func newbieResources(ctx context.Context, b *Bot, event *slack.MessageEvent, private bool) {
	newbieResources := slack.Attachment{
		Text: `First you should take the language tour: <https://tour.golang.org/>

Then, you should visit:
 - <https://golang.org/doc/code.html> to learn how to organize your Go workspace
 - <https://golang.org/doc/effective_go.html> be more effective at writing Go
 - <https://golang.org/ref/spec> learn more about the language itself
 - <https://golang.org/doc/#articles> a lot more reading material

There are some awesome websites as well:
 - <https://blog.gopheracademy.com> great resources for Gophers in general
 - <http://gotime.fm> awesome weekly podcast of Go awesomeness
 - <https://gobyexample.com> examples of how to do things in Go
 - <http://go-database-sql.org> how to use SQL databases in Go
 - <https://dmitri.shuralyov.com/idiomatic-go> tips on how to write more idiomatic Go code
 - <https://divan.github.io/posts/avoid_gotchas> will help you avoid gotchas in Go
 - <https://golangbot.com> tutorials to help you get started in Go

There's also an exhaustive list of videos <http://gophervids.appspot.com> related to Go from various authors.

If you prefer books, you can try these:
 - <http://www.golangbootcamp.com/book>
 - <http://gopl.io/>
 - <https://www.manning.com/books/go-in-action> (if you e-mail @wkennedy at bill@ardanlabs.com you can get a free copy for being part of this Slack)

If you want to learn how to organize your Go project, make sure to read: <https://medium.com/@benbjohnson/standard-package-layout-7cdbc8391fc1#.ds38va3pp>.
Once you are accustomed to the language and syntax, you can read this series of articles for a walkthrough the various standard library packages: <https://medium.com/go-walkthrough>.

Finally, <https://github.com/golang/go/wiki#learning-more-about-go> will give a list of even more resources to learn Go`,
	}

	params := slack.PostMessageParameters{AsUser: true}
	params.Attachments = []slack.Attachment{newbieResources}
	whereTo := event.Channel
	if private {
		whereTo = event.User
	}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, whereTo, "Here are some resources you should check out if you are learning / new to Go:", params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func recommendedChannels(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	message := slack.Attachment{}

	for idx, val := range b.channels {
		if val.special {
			continue
		}
		message.Text += `- <` + val.slackID + `|` + idx + `> -> ` + val.description + "\n"
	}

	params := slack.PostMessageParameters{AsUser: true}
	params.Attachments = []slack.Attachment{message}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.User, "Here is a list of recommended channels:", params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func (b *Bot) suggestPlayground(ctx context.Context, event *slack.MessageEvent) {
	if event.File == nil || b.devMode {
		return
	}

	info, _, _, err := b.slackBotAPI.GetFileInfoContext(ctx, event.File.ID, 0, 0)
	if err != nil {
		b.logf("error while getting file info: %v", err)
		return
	}

	if info.Lines < 6 || info.PrettyType == "Plain Text" {
		return
	}

	req, err := http.NewRequest("GET", info.URLPrivateDownload, nil)
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req.Header.Add("Authorization", "Bearer "+b.token)
	req = req.WithContext(ctx)

	resp, err := b.client.Do(req)
	if err != nil {
		b.logf("error while fetching the file %v\n", err)
		return
	}

	file, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		b.logf("error while reading the file %v\n", err)
		return
	}

	requestBody := bytes.NewBuffer(file)

	req, err = http.NewRequest("POST", "https://play.golang.org/share", requestBody)
	if err != nil {
		b.logf("failed to get playground link: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req.Header.Add("Content-Length", strconv.Itoa(len(file)))
	req = req.WithContext(ctx)

	resp, err = b.client.Do(req)
	if err != nil {
		b.logf("failed to get playground link: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b.logf("got non-200 response: %v", resp.StatusCode)
		return
	}

	linkID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		b.logf("failed to get playground link: %v", err)
		return
	}

	params := slack.PostMessageParameters{AsUser: true}
	_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.Channel, `The above code in playground: <https://play.golang.org/p/`+string(linkID)+`>`, params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}

	_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.User, `Hello. I've noticed you uploaded a Go file. To facilitate collaboration and make this easier for others to share back the snippet, please consider using: <https://play.golang.org>. If you wish to not link against the playground, please use "nolink" in the message. Thank you.`, params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func (b *Bot) suggestPlayground2(ctx context.Context, event *slack.MessageEvent) {
	if b.devMode {
		return
	}

	originalEventText := event.Text
	eventText := ""

	// Be nice and try to first figure out if there's any possible code in there
	/* This has a bug so don't be nice for now
	for dotPos := strings.Index(originalEventText, "```"); dotPos != -1;  dotPos = strings.Index(originalEventText, "```") {
		originalEventText = originalEventText[dotPos+3:]
		nextTripleDots := strings.Index(eventText, "```")
		if nextTripleDots == -1 {
			eventText += originalEventText
		} else {
			eventText += originalEventText[:nextTripleDots]
			originalEventText = originalEventText[nextTripleDots+3:]
		}
	}
	*/

	// Well there 's so much we can do here, the user really should have a better etiquette and not post walls of text
	if eventText == "" {
		eventText = originalEventText
	}

	requestBody := bytes.NewBufferString(eventText)

	req, err := http.NewRequest("POST", "https://play.golang.org/share", requestBody)
	if err != nil {
		b.logf("failed to get playground link: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req.Header.Add("Content-Length", strconv.Itoa(len(eventText)))

	req = req.WithContext(ctx)
	resp, err := b.client.Do(req)
	if err != nil {
		b.logf("failed to get playground link: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b.logf("got non-200 response: %v", resp.StatusCode)
		return
	}

	linkID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		b.logf("failed to get playground link: %v", err)
		return
	}

	params := slack.PostMessageParameters{AsUser: true, ThreadTimestamp: event.ThreadTimestamp}
	_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.Channel, `The above code in playground: <https://play.golang.org/p/`+string(linkID)+`>`, params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}

	_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.User, `Hello. I've noticed you've written a large block of text (more than 9 lines). To make the conversation easier to follow the conversation and facilitate collaboration, please consider using: <https://play.golang.org> if you shared code. If you wish to not link against the playground, please start the message with "nolink". Thank you.`, params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func respond(ctx context.Context, b *Bot, event *slack.MessageEvent, response string) {
	if b.devMode {
		b.logf("should reply to message %s with %s\n", event.Text, response)
		return
	}
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel, response, params)
	if err != nil {
		b.logf("%s\n", err)
	}
}

func searchLibrary(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	searchTerm := strings.ToLower(event.Text)
	if idx := strings.Index(searchTerm, "library for"); idx != -1 {
		searchTerm = event.Text[idx+11:]
	} else if idx := strings.Index(searchTerm, "library in go for"); idx != -1 {
		searchTerm = event.Text[idx+17:]
	} else if idx := strings.Index(searchTerm, "go library for"); idx != -1 {
		searchTerm = event.Text[idx+14:]
	}

	searchTerm = b.slackLinkRE.ReplaceAllString(searchTerm, "")
	searchTerm = b.emojiRE.ReplaceAllString(searchTerm, "")

	if idx := strings.Index(searchTerm, "in go"); idx != -1 {
		searchTerm = searchTerm[:idx] + searchTerm[idx+5:]
	}

	searchTerm = strings.Trim(searchTerm, "?;., ")
	if len(searchTerm) == 0 || len(searchTerm) > 100 {
		return
	}
	searchTerm = url.QueryEscape(searchTerm)
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel, `You can try to look here: <https://godoc.org/?q=`+searchTerm+`> or here <http://go-search.org/search?q=`+searchTerm+`>`, params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

var xkcdAliases = map[string]int{
	"standards":    927,
	"compiling":    303,
	"optimization": 1691,
}

func xkcd(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	// repeats some earlier work but oh well
	eventText := strings.Trim(strings.ToLower(event.Text), " \n\r")
	eventText = b.trimBot(eventText)
	eventText = strings.TrimPrefix(eventText, "xkcd:")

	// first check known aliases for certain comics
	comicID := xkcdAliases[eventText]

	// otherwise parse the number out of the evet text
	if comicID == 0 {
		// Verify it's an integer to be nice to XKCD
		num, err := strconv.Atoi(eventText)
		if err != nil {
			// pretend we didn't hear them if they give bad data
			b.logf("Error while attempting to parse XKCD string: %v\n", err)
			return
		}
		comicID = num
	}

	imageLink := fmt.Sprintf("<https://xkcd.com/%d/>", comicID)

	params := slack.PostMessageParameters{
		AsUser:      true,
		UnfurlLinks: true,
		UnfurlMedia: true,
	}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel, imageLink, params)
	if err != nil {
		b.logf("error while sending xkcd message: %s\n", err)
		return
	}
}

var regexSongNolink = regexp.MustCompile(`(?i)(nolink|song\.link)`)
var regexSongLink = regexp.MustCompile(`(i?)(open\.spotify\.com|spotify:[a-z]|soundcloud\.com|tidal\.com)/[^\s]+`)

func songLinkHandler(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	msg, params := songlink(event)
	if msg == "" {
		return
	}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel, msg, params)
	if err != nil {
		b.logf("error while replying with song link: %s\n", err)
		return
	}
}

// songlink inspects the message for Spotify, Soundcloud, Tidal links
// It returns empty string when no reply is needed
// Otherwise it returns the reply text and params configured for threaded reply
func songlink(event *slack.MessageEvent) (string, slack.PostMessageParameters) {
	if regexSongNolink.MatchString(event.Text) || !regexSongLink.MatchString(event.Text) {
		return "", slack.PostMessageParameters{}
	}
	var out string
	links := regexSongLink.FindAllString(event.Text, -1)
	for _, link := range links {
		out = fmt.Sprintf("%s\n<https://song.link/%s>", out, link)
	}
	out = strings.TrimSpace(out)

	params := slack.PostMessageParameters{
		AsUser:          true,
		UnfurlLinks:     false,
		UnfurlMedia:     false,
		ThreadTimestamp: event.ThreadTimestamp,
	}
	return out, params
}

func (b *Bot) godoc(ctx context.Context, event *slack.MessageEvent, prefix string, position int) {
	link := event.Text[position:]
	if strings.Contains(link, " ") {
		link = link[:strings.Index(link, " ")]
	}

	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel, `<https://godoc.org/`+prefix+link+`>`, params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func (b *Bot) reactToEvent(ctx context.Context, event *slack.MessageEvent, reaction string) {
	if b.devMode {
		b.logf("should reply to message %s with %s\n", event.Text, reaction)
		return
	}
	item := slack.ItemRef{
		Channel:   event.Channel,
		Timestamp: event.Timestamp,
	}
	err := b.slackBotAPI.AddReactionContext(ctx, reaction, item)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func botVersion(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.User, fmt.Sprintf("My version is: %s", b.version), params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func flipCoin(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	buff := make([]byte, 1, 1)
	_, err := rand.Read(buff)
	if err != nil {
		b.logf("%s\n", err)
	}
	result := "heads"
	if buff[0]%2 == 0 {
		result = "tail"
	}
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.Channel, fmt.Sprintf("%s", result), params)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

// NewBot will create a new Slack bot
func NewBot(slackBotAPI *slack.Client, dsClient *datastore.Client, traceClient *trace.Client, twitterAPI *anaconda.TwitterApi, httpClient Client, gerritLink, name, token, version string, devMode bool, log Logger) *Bot {
	return &Bot{
		gerritLink:  gerritLink,
		name:        name,
		token:       token,
		client:      httpClient,
		version:     version,
		devMode:     devMode,
		logf:        log,
		slackBotAPI: slackBotAPI,
		dsClient:    dsClient,
		traceClient: traceClient,
		twitterAPI:  twitterAPI,

		emojiRE:     regexp.MustCompile(`:[[:alnum:]]+:`),
		slackLinkRE: regexp.MustCompile(`<((?:@u)|(?:#c))[0-9a-z]+>`),

		channels: map[string]slackChan{
			"golang-newbies": {description: "for newbie resources", welcome: true},
			"reviews":        {description: "for code reviews", welcome: true},
			"gotimefm":       {description: "for the awesome live podcast", welcome: true},
			"remotemeetup":   {description: "for remote meetup", welcome: true},
			"golang-jobs":    {description: "for jobs related to Go", welcome: true},

			"showandtell": {description: "tell the world about the thing you are working on"},
			"performance": {description: "anything and everything performance related"},
			"devops":      {description: "for devops related discussions"},
			"security":    {description: "for security related discussions"},
			"aws":         {description: "if you are interested in AWS"},
			"goreviews":   {description: "talk to the Go team about a certain CL"},
			"golang-cls":  {description: "get real time udates from the merged CL for Go itself. For a currated list of important / interesting messages follow: https://twitter.com/golang_cls"},
			"bbq":         {description: "Go controlling your bbq grill? Yes, we have that"},

			"general":    {description: "general channel", special: true},
			"golang_cls": {description: "https://twitter.com/golang_cls", special: true},
		},
	}
}
