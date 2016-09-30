package bot

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/nlopes/slack"
)

type (
	slackChan struct {
		description string
		slackID     string
		special     bool
	}

	botReply func(event *slack.MessageEvent) []string

	command struct {
		toBotOnly      bool
		matcher        func(event *slack.MessageEvent) bool
		react          botReply
		message        botReply
		privateMessage botReply
	}

	Client interface {
		Do(r *http.Request) (*http.Response, error)
	}

	Logger func(message string, args ...interface{})

	Bot struct {
		id             string
		gerritLink     string
		name           string
		token          string
		version        string
		dlsniperUserID string
		client         Client
		devMode        bool
		emojiRE        *regexp.Regexp
		slackLinkRE    *regexp.Regexp
		channels       map[string]slackChan
		slackAPI       *slack.Client

		log Logger
	}
)

func (b *Bot) Init(rtm *slack.RTM) error {
	b.log("Determining bot / dlsniper user ID")
	users, err := b.slackAPI.GetUsers()
	if err != nil {
		return err
	}

	for _, user := range users {
		if !user.IsBot {
			if user.Name == "dlsniper" {
				b.dlsniperUserID = user.ID
			}
			continue
		}

		if user.Name == b.name {
			b.id = user.ID
			break
		}
	}
	if b.id == "" {
		return errors.New("could not find bot in the list of names, check if the bot is called \"" + b.name + "\" ")
	}

	b.log("Determining channels ID\n")
	publicChannels, err := b.slackAPI.GetChannels(true)
	if err != nil {
		return err
	}

	for _, channel := range publicChannels {
		if chn, ok := b.channels[channel.Name]; ok {
			chn.slackID = "#" + channel.ID
			b.channels[channel.Name] = chn
		}
	}

	b.log("Determining groups ID\n")
	botGroups, err := b.slackAPI.GetGroups(true)
	for _, group := range botGroups {
		if chn, ok := b.channels[group.Name]; ok && b.channels[group.Name].slackID == "" {
			chn.slackID = "#" + group.Name
			b.channels[group.Name] = chn
		}
	}

	b.log("Initialized %s with ID: %s\n", b.name, b.id)

	params := slack.PostMessageParameters{AsUser: true}
	_, _, err = b.slackAPI.PostMessage(b.dlsniperUserID, fmt.Sprintf(`Deployed version: %s`, b.version), params)

	if err == nil {
		go b.MonitorGerrit()
	}
	return err
}

func (b *Bot) TeamJoined(event *slack.TeamJoinEvent) {
	message := `Hello ` + event.User.Name + `,


Welcome to the Gophers Slack channel.

This Slack is meant to connect gophers from all over the world in a central place.

We have a few rules that you can see here: http://coc.golangbridge.org.
One of the rules is that we ask you to have a full name, the name people know you as, regardless of whether that corresponds with your ID or not.
so that other gophers can recognize you easier.

There is also a forum: https://forum.golangbridge.org, you might want to check it out as well.

If you are new to Go and want a copy of the Go In Action book, https://www.manning.com/books/go-in-action, please send an email to @wkennedy at bill@ardanlabs.com

Here's a list of a few channels you could join:
`

	for idx, val := range b.channels {
		if val.special {
			continue
		}
		message += `<` + val.slackID + `|` + idx + `> -> ` + val.description + "\n"
	}

	message += `
There are quite a few other channels, depending on your interests or location (we have city / country wide channels).
Just click on the channel list and search for anything that crosses your mind.

To share code, you should use: https://play.golang.org/ as it makes it easy for others to help you.

Final thing, #general might be too chatty at times but don't be shy to ask your Go related question.


Now enjoy your stay and have fun.`

	params := slack.PostMessageParameters{AsUser: true, LinkNames: 1}
	_, _, err := b.slackAPI.PostMessage(event.User.ID, message, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) HandleMessage(event *slack.MessageEvent) {
	if event.BotID != "" || event.User == "" || event.SubType == "bot_message" {
		return
	}

	eventText := strings.ToLower(event.Text)

	if b.devMode {
		b.log("got message: %s\n", eventText)
	}

	if strings.Contains(eventText, "newbie resources") &&
		!(strings.Contains(eventText, strings.ToLower("@"+b.name)) || strings.Contains(eventText, strings.ToLower(b.id))) {
		b.NewbieResources(event, false)
		return
	}

	// TODO should we check for ``` or messages of a certain length?
	if !strings.Contains(eventText, "nolink") &&
		event.File != nil &&
		(event.File.Filetype == "go" || event.File.Filetype == "text") {
		b.SuggestPlayground(event)
		return
	}

	// All the variations of table flip seem to include this characters so... potato?
	if strings.Contains(eventText, "︵") || strings.Contains(eventText, "彡") {
		b.TableUnflip(event)
		return
	}

	if strings.Contains(eventText, "oss help") {
		b.OSSHelp(event)
		return
	}

	if strings.Contains(eventText, "work with go forks") {
		b.GoForks(event)
		return
	}

	if strings.Contains(eventText, "block forever in go") {
		b.GoBlockForever(event)
		return
	}

	if strings.HasPrefix(eventText, "hi guys") ||
		strings.HasPrefix(eventText, "hey guys") ||
		strings.HasPrefix(event.Text, "hello guys") {
		b.GophersNotGuys(event)
		return
	}

	if strings.Contains(eventText, "my adorable little gophers") {
		b.ReactToEvent(event, "gopher")
		return
	}

	if strings.Contains(eventText, "bbq") {
		b.ReactToEvent(event, "bbqgopher")
		return
	}

	if strings.Contains(eventText, "ermergerd") ||
		strings.Contains(eventText, "ermahgerd") {
		b.ReactToEvent(event, "dragon")
		return
	}

	if strings.Contains(eventText, "blog about http timeouts") {
		b.DealWithHTTPTimeouts(event)
		return
	}

	if strings.Contains(eventText, "go database tutorial") {
		b.GoDatabaseTutorial(event)
		return
	}

	if strings.Contains(eventText, "xkcd:standards") {
		b.XKCD(event, "https://xkcd.com/927/")
		return
	}

	if strings.Contains(eventText, "xkcd:compiling") {
		b.XKCD(event, "https://xkcd.com/303/")
		return
	}

	if strings.Contains(eventText, "xkcd:optimization") {
		b.XKCD(event, "https://xkcd.com/1691/")
		return
	}

	if strings.HasPrefix(eventText, "ghd/") {
		b.Godoc(event, "github.com/", 4)
		return
	}

	if strings.HasPrefix(eventText, "d/") {
		b.Godoc(event, "", 2)
		return
	}

	if strings.Contains(eventText, strings.ToLower("@"+b.name)) || strings.Contains(eventText, strings.ToLower(b.id)) {
		if strings.Contains(eventText, "package layout") {
			b.PackageLayout(event)
			return
		}

		if strings.Contains(eventText, "library for") ||
			strings.Contains(eventText, "library in go for") ||
			strings.Contains(eventText, "go library for") {
			b.SearchLibrary(event)
			return
		}

		if strings.Contains(eventText, "thank") ||
			strings.Contains(eventText, "cheers") ||
			strings.Contains(eventText, "hello") {
			b.ReactToEvent(event, "gopher")
			return
		}

		if strings.Contains(eventText, "wave") {
			b.ReactToEvent(event, "wave")
			b.ReactToEvent(event, "gopher")
			return
		}

		if strings.Contains(eventText, "flip coin") ||
			strings.Contains(eventText, "flip a coin") {
			b.ReplyFlipCoin(event)
			return
		}

		if strings.Contains(eventText, "where do you live?") {
			b.ReplyBotLocation(event)
			return
		}

		if strings.Contains(eventText, "version") {
			b.ReplyVersion(event)
			return
		}

		if strings.Contains(eventText, "newbie resources") {
			b.NewbieResources(event, true)
			return
		}

		return
	}
}

func (b *Bot) NewbieResources(event *slack.MessageEvent, private bool) {
	newbieResources := slack.Attachment{
		Text: `First you should take the language tour: <http://tour.golang.org/>

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
 - <http://gophervids.appspot.com> list of Go related videos from various authors

If you prefer books, you can try these:
 - <http://www.golangbootcamp.com/book>
 - <http://gopl.io/>
 - <https://www.manning.com/books/go-in-action> (if you e-mail @wkennedy at bill@ardanlabs.com you can get a free copy for being part of this Slack)

If you want to learn how to organize your Go project, make sure to read: <https://medium.com/@benbjohnson/standard-package-layout-7cdbc8391fc1#.ds38va3pp>.
Once you are acustomed to the language and syntax, you can read this series of articles for a walkthrough the various standard library packages: <https://medium.com/go-walkthrough>.

Finally, <https://github.com/golang/go/wiki#learning-more-about-go> will give a list of even more resources to learn Go`,
	}

	params := slack.PostMessageParameters{AsUser: true}
	params.Attachments = []slack.Attachment{newbieResources}
	whereTo := event.Channel
	if private {
		whereTo = event.User
	}
	_, _, err := b.slackAPI.PostMessage(whereTo, "Here are some resources you should check out if you are learning / new to Go:", params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) SuggestPlayground(event *slack.MessageEvent) {
	if event.File == nil {
		return
	}

	info, _, _, err := b.slackAPI.GetFileInfo(event.File.ID, 0, 0)
	if err != nil {
		b.log("error while getting file info: %v", err)
		return
	}

	if info.Lines < 6 {
		return
	}

	req, err := http.NewRequest("GET", info.URLPrivateDownload, nil)
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req.Header.Add("Authorization", "Bearer "+b.token)
	resp, err := b.client.Do(req)
	if err != nil {
		b.log("error while fetching the file %v\n", err)
		return
	}

	file, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		b.log("error while reading the file %v\n", err)
		return
	}

	requestBody := bytes.NewBuffer(file)

	req, err = http.NewRequest("POST", "https://play.golang.org/share", requestBody)
	if err != nil {
		b.log("failed to get playground link: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("User-Agent", "Gophers Slack bot")
	req.Header.Add("Content-Length", strconv.Itoa(len(file)))

	resp, err = b.client.Do(req)
	if err != nil {
		b.log("failed to get playground link: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b.log("got non-200 response: %v", resp.StatusCode)
		return
	}

	linkID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		b.log("failed to get playground link: %v", err)
		return
	}

	params := slack.PostMessageParameters{AsUser: true}
	_, _, err = b.slackAPI.PostMessage(event.Channel, `The above code in playground: <https://play.golang.org/p/`+string(linkID)+`>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}

	_, _, err = b.slackAPI.PostMessage(event.User, `Hello. I've noticed you uploaded a Go file. To enable collaboration and make this easier to get help, please consider using: <https://play.golang.org>. Thank you.`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) OSSHelp(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, `Here's a list of projects which could need some help from contributors like you: <https://github.com/corylanou/oss-helpwanted>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) GoForks(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, `<http://blog.sgmansfield.com/2016/06/working-with-forks-in-go/>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) GoBlockForever(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, `<http://blog.sgmansfield.com/2016/06/how-to-block-forever-in-go/>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) GoDatabaseTutorial(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, `<http://go-database-sql.org/>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) GophersNotGuys(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.User, `Hi please use gophers or folks not guys. Thank you :)`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) DealWithHTTPTimeouts(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, `Here's a blog post which will help with http timeouts in Go: <https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) TableUnflip(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, `┬─┬ノ( º _ ºノ)`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) SearchLibrary(event *slack.MessageEvent) {
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
	_, _, err := b.slackAPI.PostMessage(event.Channel, `You can try to look here: <https://godoc.org/?q=`+searchTerm+`> or here <http://go-search.org/search?q=`+searchTerm+`>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) XKCD(event *slack.MessageEvent, imageLink string) {
	params := slack.PostMessageParameters{AsUser: true, UnfurlLinks: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, imageLink, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) Godoc(event *slack.MessageEvent, prefix string, position int) {
	link := event.Text[position:]
	if strings.Contains(link, " ") {
		link = link[:strings.Index(link, " ")]
	}

	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, `<https://godoc.org/`+prefix+link+`>`, params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) ReactToEvent(event *slack.MessageEvent, reaction string) {
	item := slack.ItemRef{
		Channel:   event.Channel,
		Timestamp: event.Timestamp,
	}
	err := b.slackAPI.AddReaction(reaction, item)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) ReplyVersion(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.User, fmt.Sprintf("My version is: %s", b.version), params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) ReplyBotLocation(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, "I'm currently living in the Clouds, powered by Google Container Engine (GKE) <https://cloud.google.com/container-engine>. I find my way to home using CircleCI <https://circleci.com> and Kubernetes (k8s) <http://kubernetes.io>. You can find my heart at: <https://github.com/gopheracademy/gopher>.", params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) ReplyFlipCoin(event *slack.MessageEvent) {
	buff := make([]byte, 1, 1)
	_, err := rand.Read(buff)
	if err != nil {
		b.log("%s\n", err)
	}
	result := "heads"
	if buff[0]%2 == 0 {
		result = "tail"
	}
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err = b.slackAPI.PostMessage(event.Channel, fmt.Sprintf("%s", result), params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func (b *Bot) PackageLayout(event *slack.MessageEvent) {
	params := slack.PostMessageParameters{AsUser: true}
	_, _, err := b.slackAPI.PostMessage(event.Channel, "This article will explain how to organize your Go packages <https://medium.com/@benbjohnson/standard-package-layout-7cdbc8391fc1#.ds38va3pp>", params)
	if err != nil {
		b.log("%s\n", err)
		return
	}
}

func NewBot(slackAPI *slack.Client, httpClient Client, gerritLink, name, token, version string, devMode bool, log Logger) *Bot {
	return &Bot{
		gerritLink: gerritLink,
		name:       name,
		token:      token,
		client:     httpClient,
		version:    version,
		devMode:    devMode,
		log:        log,
		slackAPI:   slackAPI,

		emojiRE:     regexp.MustCompile(`:[[:alnum:]]+:`),
		slackLinkRE: regexp.MustCompile(`<((?:@u)|(?:#c))[0-9a-z]+>`),

		channels: map[string]slackChan{
			"golang-newbies": {description: "for newbie resources"},
			"reviews":        {description: "for code reviews"},
			"showandtell":    {description: "tell the world about the thing you are working on"},
			"golang-jobs":    {description: "for jobs related to Go"},
			"golang_cls":     {description: "https://twitter.com/golang_cls", special: true},
			// TODO add more channels to share with the newbies?
		},
	}
}
