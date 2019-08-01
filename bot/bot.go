package bot

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/trace"
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
		logf        Logger
		dsClient    *datastore.Client
		traceClient *trace.Client
		opsChannel  string

		goTimeLastNotified time.Time
	}

	slackHandler func(context.Context, *Bot, *slack.MessageEvent)
)

var welcomeMessage = ""

func (b *Bot) getID(ctx context.Context) (string, error) {
	ai, err := b.slackBotAPI.AuthTestContext(ctx)
	if err != nil {
		return "U1XK0CWSZ", err // This is the old hard coded id
	}
	return ai.UserID, nil
}

// Init must be called before anything else in order to initialize the bot
func (b *Bot) Init(ctx context.Context, rtm *slack.RTM, span *trace.Span, opsChannel string) error {
	initSpan := span.NewChild("b.Init")
	defer initSpan.Finish()

	b.opsChannel = opsChannel

	b.logf("Determining bot / user IDs")

	var err error
	b.id, err = b.getID(ctx)
	if err != nil {
		b.logf("Error getting bot user id: %s\n", err)
	}

	b.msgprefix = strings.ToLower("<@" + b.id + ">")

	b.logf("Determining channels ID\n")
	channels, err := b.listChannels(ctx, initSpan)
	if err != nil {
		return err
	}

	for _, channel := range channels {
		channelName := strings.ToLower(channel.Name)
		if chn, ok := b.channels[channelName]; ok {
			chn.slackID = "#" + channel.ID
			b.channels[channelName] = chn
		}
	}

	for name, channel := range b.channels {
		b.logf("Channel %q -> ID %q", name, channel.slackID)
	}

	b.logf("Initialized %s with ID (%q) and msgprefix (%q) \n", b.name, b.id, b.msgprefix)
	if b.opsChannel != "" {
		childSpan := initSpan.NewChild("b.AnnouncingStartupFinish")
		_, _, err = b.slackBotAPI.PostMessageContext(ctx, b.opsChannel,
			slack.MsgOptionAsUser(true),
			slack.MsgOptionText(`Deployed version: `+b.version, false),
		)
		childSpan.Finish()
		if err != nil {
			b.logf(`failed to deploy version: %s`, b.version)
		}
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

func (b *Bot) listChannels(ctx context.Context, span *trace.Span) ([]slack.Channel, error) {
	childSpan := span.NewChild("slackApi.GetConversations")
	childSpan.Finish()

	params := &slack.GetConversationsParameters{
		ExcludeArchived: "true",
		Limit:           200,
		Types: []string{
			"public_channel",
			"private_channl",
		},
	}

	channels, nextCursor, err := b.slackBotAPI.GetConversationsContext(ctx, params)
	if err != nil {
		return nil, err
	}
	params.Cursor = nextCursor

	for params.Cursor != "" {
		var pageChannels []slack.Channel
		pageChannels, params.Cursor, err = b.slackBotAPI.GetConversationsContext(ctx, params)
		if err != nil {
			return nil, err
		}
		channels = append(channels, pageChannels...)
	}

	return channels, nil
}

// TeamJoined is called when the someone joins the team
func (b *Bot) TeamJoined(event *slack.TeamJoinEvent) {
	span := b.traceClient.NewSpan("b.TeamJoined")
	defer span.Finish()

	/*
		if b.devMode {
			return
		}
	*/

	message := `Hello ` + event.User.Name + welcomeMessage

	ctx := trace.NewContext(context.Background(), span)
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.User.ID,
		slack.MsgOptionPostMessageParameters(slack.PostMessageParameters{LinkNames: 1}),
		slack.MsgOptionAsUser(true),
		slack.MsgOptionText(message, false),
	)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func (b *Bot) isBotMessage(event *slack.MessageEvent, eventText string) bool {
	prefixes := []string{
		b.msgprefix,
		"gopher", // emoji :gopher: or text `gopher`
	}

	for _, p := range prefixes {
		if strings.HasPrefix(eventText, p) {
			return true
		}
	}

	return strings.HasPrefix(event.Channel, "D") // direct message
}

func (b *Bot) trimBot(msg string) string {
	msg = strings.Replace(msg, strings.ToLower(b.msgprefix), "", 1)
	msg = strings.TrimPrefix(msg, "gopher")
	msg = strings.Trim(msg, " :\n")

	return msg
}

var (
	// Generic responses to all messages
	containsToReactions = map[string]struct {
		reactions []string
		rand      bool
	}{
		"︵":                          {reactions: []string{"┬─┬ノ( º _ ºノ)"}},
		"彡":                          {reactions: []string{"┬─┬ノ( º _ ºノ)"}},
		"my adorable little gophers": {reactions: []string{"gopher"}},
		"bbq":                        {reactions: []string{"bbqgopher"}},
		"buffalo":                    {reactions: []string{"gobuffalo"}},
		"gobuffalo":                  {reactions: []string{"gobuffalo"}},
		"ghost":                      {reactions: []string{"ghost"}},
		"ermergerd":                  {reactions: []string{"dragon"}},
		"ermahgerd":                  {reactions: []string{"dragon"}},
		"dragon":                     {reactions: []string{"dragon"}},
		"spacex":                     {reactions: []string{"rocket"}},
		"beer me":                    {reactions: []string{"beer", "beers"}},
		"spacemacs":                  {reactions: []string{"spacemacs"}},
		"emacs":                      {reactions: []string{"vim"}, rand: true},
		"vim":                        {reactions: []string{"emacs"}, rand: true},
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
		"where do you live?":   stack,
		"stack":                stack,
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
		"books": {
			`Here are some popular books you can use to get started:`,
			`- William Kennedy, Brian Ketelsen, Erik St. Martin Go In Action <https://www.manning.com/books/go-in-action>`,
			`- Alan A A Donovan, Brian W Kernighan The Go Programming Language <https://www.gopl.io>`,
			`- Mat Ryer Go Programming Blueprints 2nd Edition <https://www.packtpub.com/application-development/go-programming-blueprints-second-edition>`,
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
			`- <https://blog.golang.org/go-slices-usage-and-internals>`,
			`- <https://blog.golang.org/slices>`,
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
			`My source code is here <https://github.com/gobridge/gopher>`,
		},
		"dependency injection": {
			`If you'd like to learn more about how to use Dependency Injection in Go, please review this post:`,
			`- <https://appliedgo.net/di/>`,
		},
		"pointer performance": {
			`The answer to whether using a pointer offers a performance gain is complex and is not always the case. Please read these posts for more information:`,
			`- <https://medium.com/@vCabbage/go-are-pointers-a-performance-optimization-a95840d3ef85>`,
			`- <https://segment.com/blog/allocation-efficiency-in-high-performance-go-services/>`,
		},
		"help": {
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
		},
		"gopath": {
			"Your project should be structured as follows:",
			"```GOPATH=~/go",
			"~/go/src/sourcecontrol/username/project/```",
			"Whilst you _can_ get around the GOPATH, it's ill-advised. Read more about the GOPATH here: https://github.com/golang/go/wiki/GOPATH",
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
		"package structure":    "package layout",
		"project structure":    "package layout",
		"project layout":       "package layout",
		"di":                   "dependency injection",
		"gopath problem":       "gopath",
		"issue with gopath":    "gopath",
		"help with gopath":     "gopath",
	}

	botPrefixToFunc = map[string]slackHandler{
		"xkcd:":       xkcd,
		"library for": searchLibrary,
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
	}

	span := b.traceClient.NewSpan("b.HandleMessage")
	span.SetLabel("eventText", eventText)
	defer span.Finish()

	ctx := trace.NewContext(context.Background(), span)

	// On messages containing song links, share alternate music platforms in thread
	songLinkHandler(ctx, b, event)

	// Reactions to all messages (including those not directed at the bot)
	// that contain a certain string
	for needle, tr := range containsToReactions {
		if strings.Contains(eventText, needle) {
			if _, ok := reactWithMessage[needle]; ok {
				if !tr.rand || rand.Intn(150) == 0x2A {
					for _, reaction := range tr.reactions {
						respond(ctx, b, event, reaction)
					}
				}
			} else {
				if !tr.rand || rand.Intn(150) == 0x2A {
					for _, reaction := range tr.reactions {
						b.reactToEvent(ctx, event, reaction)
					}
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

	if event.Upload && !strings.Contains(eventText, "nolink") {
		for _, file := range event.Files {
			if file.Filetype == "go" || file.Filetype == "text" {
				b.suggestPlayground(ctx, event)
				break
			}
		}
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

	whereTo := event.Channel
	if private {
		whereTo = event.User
	}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, whereTo,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionText("Here are some resources you should check out if you are learning / new to Go:", false),
		slack.MsgOptionAttachments(newbieResources),
	)
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

	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.User,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionText("Here is a list of recommended channels:", false),
		slack.MsgOptionAttachments(message),
	)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func (b *Bot) suggestPlayground(ctx context.Context, event *slack.MessageEvent) {
	if len(event.Files) == 0 {
		return
	}

	for _, file := range event.Files {
		info, _, _, err := b.slackBotAPI.GetFileInfoContext(ctx, file.ID, 0, 0)
		if err != nil {
			b.logf("error while getting file info: %v", err)
			return
		}

		if info.Lines < 6 || info.PrettyType == "Plain Text" {
			return
		}

		req, err := http.NewRequest("GET", info.URLPrivateDownload, nil)
		if err != nil {
			b.logf("error creating playground request to %q: %v\n", info.URLPrivateDownload, err)
			return
		}
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

		_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.Channel,
			slack.MsgOptionAsUser(true),
			slack.MsgOptionText(`The above code in playground: <https://play.golang.org/p/`+string(linkID)+`>`, false),
		)
		if err != nil {
			b.logf("%s\n", err)
			return
		}
	}

	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.User,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionText(`Hello. I've noticed you uploaded a Go file. To facilitate collaboration and make this easier for others to share back the snippet, please consider using: <https://play.golang.org>. If you wish to not link against the playground, please use "nolink" in the message. Thank you.`, false),
	)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func (b *Bot) suggestPlayground2(ctx context.Context, event *slack.MessageEvent) {
	/* if b.devMode {
		return
	}*/

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

	_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.Channel,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionTS(event.ThreadTimestamp),
		slack.MsgOptionText(`The above code in playground: <https://play.golang.org/p/`+string(linkID)+`>`, false),
	)
	if err != nil {
		b.logf("%s\n", err)
		return
	}

	_, _, err = b.slackBotAPI.PostMessageContext(ctx, event.User,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionText(`Hello. I've noticed you've written a large block of text (more than 9 lines). To make the conversation easier to follow the conversation and facilitate collaboration, please consider using: <https://play.golang.org> if you shared code. If you wish to not link against the playground, please start the message with "nolink". Thank you.`, false),
	)
	if err != nil {
		b.logf("%s\n", err)
		return
	}
}

func respond(ctx context.Context, b *Bot, event *slack.MessageEvent, response string) {
	if b.devMode {
		b.logf("should reply to message %s with %s\n", event.Text, response)
	}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionTS(event.ThreadTimestamp),
		slack.MsgOptionText(response, false),
	)
	if err != nil {
		b.logf("%s\n", err)
	}
}

func respondUnfurled(ctx context.Context, b *Bot, event *slack.MessageEvent, response string) {
	if b.devMode {
		b.logf("should reply to message %s with %s\n", event.Text, response)
	}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionTS(event.ThreadTimestamp),
		slack.MsgOptionEnableLinkUnfurl(),
		slack.MsgOptionText(response, false),
	)
	if err != nil {
		b.logf("%s\n", err)
	}
}

var (
	libraryText = []string{"library for", "library in go for", "go library for"}
)

func searchLibrary(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	searchTerm := strings.ToLower(event.Text)
	for _, t := range libraryText {
		if idx := strings.Index(searchTerm, t); idx != -1 {
			searchTerm = event.Text[idx+len(t):]
			break
		}
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
	respond(ctx, b, event, `You can try to look here: <https://godoc.org/?q=`+searchTerm+`> or here <http://go-search.org/search?q=`+searchTerm+`>`)
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

	respondUnfurled(ctx, b, event, fmt.Sprintf("<https://xkcd.com/%d/>", comicID))
}

var regexSongNolink = regexp.MustCompile(`(?i)(nolink|song\.link)`)
var regexSongLink = regexp.MustCompile(`(?i)(?:https?://)?(open\.spotify\.com/|spotify:|soundcloud\.com/|tidal\.com/)[^\s]+`)

func songLinkHandler(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	msg := songlink(event)
	if msg == "" {
		return
	}
	_, _, err := b.slackBotAPI.PostMessageContext(ctx, event.Channel,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionTS(event.ThreadTimestamp),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
		slack.MsgOptionText(msg, false),
	)
	if err != nil {
		b.logf("error while replying with song link: %s\n", err)
		return
	}
}

// songlink inspects the message for Spotify, Soundcloud, Tidal links
// It returns empty string when no reply is needed
// Otherwise it returns the reply text and params configured for threaded reply
func songlink(event *slack.MessageEvent) string {
	if regexSongNolink.MatchString(event.Text) || !regexSongLink.MatchString(event.Text) {
		return ""
	}

	var out string
	links := regexSongLink.FindAllString(event.Text, -1)
	for _, link := range links {
		out = fmt.Sprintf("%s\n<https://song.link/%s>", out, link)
	}
	out = strings.TrimSpace(out)

	return out
}

func (b *Bot) godoc(ctx context.Context, event *slack.MessageEvent, prefix string, position int) {
	link := event.Text[position:]
	if strings.Contains(link, " ") {
		link = link[:strings.Index(link, " ")]
	}
	link = `<https://godoc.org/` + prefix + link + `>`
	respond(ctx, b, event, link)
}

func (b *Bot) reactToEvent(ctx context.Context, event *slack.MessageEvent, reaction string) {
	if b.devMode {
		b.logf("should reply to message %s with %s\n", event.Text, reaction)
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

func stack(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	var msg string
	dyno := os.Getenv("DYNO")
	switch {
	case len(dyno) >= 3:
		msg = `I'm currently powered by Heroku <https://heroku.com>.`
	default:
		msg = `I'm currently powered by Google Container Engine (GKE) <https://cloud.google.com/container-engine> and Kubernetes (k8s) <http://kubernetes.io>.`
	}
	msg += "\nYou can find my source code at: <https://github.com/gobridge/gopher>."
	respond(ctx, b, event, msg)
}

func botVersion(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	respond(ctx, b, event, fmt.Sprintf("My version is: %s", b.version))
}

func flipCoin(ctx context.Context, b *Bot, event *slack.MessageEvent) {
	var msg string
	val := rand.Intn(2)
	switch val {
	case 0:
		msg = "heads"
	case 1:
		msg = "tails"
	default:
		panic(fmt.Sprintf("expected rand.Intn(2) to be 0 or 1, got %d", val))
	}
	respond(ctx, b, event, msg)
}

// NewBot will create a new Slack bot
func NewBot(slackBotAPI *slack.Client, dsClient *datastore.Client, traceClient *trace.Client, httpClient Client, gerritLink, name, token, version string, devMode bool, log Logger) *Bot {
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

		emojiRE:     regexp.MustCompile(`:[[:alnum:]]+:`),
		slackLinkRE: regexp.MustCompile(`<((?:@u)|(?:#c))[0-9a-z]+>`),

		users: map[string]string{},

		channels: map[string]slackChan{
			"general":      {description: "for general Go questions or help", welcome: true},
			"newbies":      {description: "for newbie resources", welcome: true},
			"reviews":      {description: "for code reviews", welcome: true},
			"gotimefm":     {description: "for the awesome live podcast", welcome: true},
			"remotemeetup": {description: "for remote meetup", welcome: true},
			"showandtell":  {description: "for telling the world about the thing you are working on", welcome: true},
			"jobs":         {description: "for jobs related to Go", welcome: true},

			"performance": {description: "anything and everything performance related"},
			"devops":      {description: "for devops related discussions"},
			"security":    {description: "for security related discussions"},
			"aws":         {description: "if you are interested in AWS"},
			"goreviews":   {description: "talk to the Go team about a certain CL"},
			"golang-cls":  {description: "get real time udates from the merged CL for Go itself"},
			"bbq":         {description: "Go controlling your bbq grill? Yes, we have that"},

			"announcements": {description: "community / ecosystem announcements channel", special: true},
		},
	}
}
