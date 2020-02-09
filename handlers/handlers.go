package handlers

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/gobridge/gopher/bot"
)

// Channel describes a Slack channel.
type Channel struct {
	Name        string
	Description string
}

	// Condition will check whether a message matches a condition.
	Condition func(bot.Message, []string) bool
)

var (
	// Exact will return true if a message contains an exact match to one
	// or more strings.
	Exact Condition = func(m bot.Message, strs []string) bool {
		for _, str := range strs {
			if m.TrimmedText == str {
				return true
			}
		}
		return false
	}
	// Contains will return true if a message contains an partial match to one
	// or more strings.
	Contains Condition = func(m bot.Message, strs []string) bool {
		for _, str := range strs {
			if strings.Contains(m.Event.Text, str) {
				return true
			}
		}
		return false
	}
	// HasPrefix will return true if a message begins with one or more strings.
	HasPrefix Condition = func(m bot.Message, strs []string) bool {
		for _, str := range strs {
			if strings.HasPrefix(m.Event.Text, str) {
				return true
			}
		}
		return false
	}
)

// ProcessLinear calls handlers in order.
func ProcessLinear(hs ...bot.Handler) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		for _, h := range hs {
			h.Handle(ctx, m, r)
		}
	})
}

// RespondWhenContains responds to any message when that contains s.
func RespondWhenContains(s string, response string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if strings.Contains(m.Event.Text, s) {
			r.Respond(ctx, response)
		}
	})
}

// WhenDirectedToBot calls h when Message.DirectedToBot is true.
func WhenDirectedToBot(h bot.Handler) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !m.DirectedToBot {
			return
		}
		h.Handle(ctx, m, r)
	})
}

// respond checks messages for matches and responds with a string if matched.
func respond(isMatch Condition, prompts []string, response string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !isMatch(m, prompts) {
			return
		}
		r.Respond(ctx, response)
	})
}

// RespondWhenContains responds if a message contains one or more strings.
func RespondWhenContains(s []string, response string) bot.Handler {
	return respond(Contains, s, response)
}

// RespondWhenExact responds if exact strings are present in a message.
func RespondWhenExact(s []string, response string) bot.Handler {
	return respond(Exact, s, response)
}

// react adds reactions to messages when string(s) meet a condition.
func react(isMatch Condition, s []string, reactions ...string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		for _, reaction := range reactions {
			if !isMatch(m, []string{reaction}) {
				continue
			}
			r.React(ctx, reaction)
		}
	})
}

// ReactWhenContains adds reactions to messages that contain s.
func ReactWhenContains(s string, reactions ...string) bot.Handler {
	return react(Contains, []string{s}, reactions...)
}

// ReactWhenHasPrefix addes reactions to messages when Message.TrimmedText has prefix s.
func ReactWhenHasPrefix(s string, reactions ...string) bot.Handler {
	return react(HasPrefix, []string{s}, reactions...)
}

// ReactWhenContainsRand randomly calls ReactWhenContains.
//
// Currently probability is 1/150.
func ReactWhenContainsRand(s string, reactions ...string) bot.Handler {
	h := ReactWhenContains(s, reactions...)
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		// TODO: make probability configurable?
		if !(rand.Intn(150) == 0x2A) {
			return
		}
		h.Handle(ctx, m, r)
	})
}

// BotStack responds to messages with information about the bot and where
// it runs when Message.TrimmedText contains one of prompts.
func BotStack(prompts []string) bot.Handler {
	var msg string
	dyno := os.Getenv("DYNO")
	switch {
	case len(dyno) >= 3:
		msg = `I'm currently powered by Heroku <https://heroku.com>.`
	default:
		msg = `I'm currently powered by Google Container Engine (GKE) <https://cloud.google.com/container-engine> and Kubernetes (k8s) <http://kubernetes.io>.`
	}
	msg += "\nYou can find my source code at: <https://github.com/gobridge/gopher>."

	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !Exact(m, prompts) {
			return
		}
		r.Respond(ctx, msg)
	})
}

// BotVersion responds to messages with the bot's version when Message.TrimmedText
// matches prompt.
func BotVersion(prompt, version string) bot.Handler {
	msg := "My version is: " + version
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !Exact(m, []string{prompt}) {
			return
		}
		r.Respond(ctx, msg)
	})
}

// CoinFlip responds to messages with "heads" or "tails" when Message.TrimmedText
// matches one of prompts.
func CoinFlip(prompts []string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !Exact(m, prompts) {
			return
		}
		if rand.Intn(2) == 0 {
			r.Respond(ctx, "heads")
		} else {
			r.Respond(ctx, "tails")
		}
	})
}

// RecommendedChannels responds to messages a formatted list of channels when
// Message.TrimmedText matches prompt.
func RecommendedChannels(prompt string, channels []Channel) bot.Handler {
	var recommendedChannels string
	for _, c := range channels {
		recommendedChannels += fmt.Sprintf("- #%s -> %s\n", c.Name, c.Description)
	}

	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if Exact(m, []string{prompt}) {
			r.RespondWithAttachment(ctx, "Here is a list of recommended channels:", recommendedChannels)
		}

		r.RespondWithAttachment(ctx, "Here is a list of recommended channels:", recommendedChannels)
	})
}

// SearchForLibrary responds to messages with suggested places to look for
// a library when Message.TrimmedText has prefix.
func SearchForLibrary(prefix string) bot.Handler {
	var (
		emojiRE     = regexp.MustCompile(`:[[:alnum:]]+:`)
		slackLinkRE = regexp.MustCompile(`<((?:@u)|(?:#c))[0-9a-z]+>`)
	)

	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !HasPrefix(m, []string{prefix}) {
			return
		}

		searchTerm := strings.TrimPrefix(m.TrimmedText, prefix)
		searchTerm = slackLinkRE.ReplaceAllString(searchTerm, "")
		searchTerm = emojiRE.ReplaceAllString(searchTerm, "")
		searchTerm = strings.Trim(searchTerm, "?;., ")
		if len(searchTerm) == 0 || len(searchTerm) > 100 {
			return
		}

		searchTerm = url.QueryEscape(searchTerm)
		r.Respond(ctx, `You can try to look here: <https://godoc.org/?q=`+searchTerm+`> or here <http://go-search.org/search?q=`+searchTerm+`>`)
	})
}

// XKCD responds with XKCD comics when Message.TrimmedText has prefix.
//
// After the prefix either a comic number or alias can be provided.
func XKCD(prefix string, aliases map[string]int, logf bot.Logger) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !HasPrefix(m, []string{prefix}) {
			return
		}

		eventText := strings.TrimPrefix(m.TrimmedText, prefix)

		// first check known aliases for certain comics
		// otherwise parse the number out of the evet text
		comicID, ok := aliases[eventText]
		if !ok {
			// Verify it's an integer to be nice to XKCD
			num, err := strconv.Atoi(eventText)
			if err != nil {
				// pretend we didn't hear them if they give bad data
				logf("Error while attempting to parse XKCD string: %v\n", err)
				return
			}
			comicID = num
		}

		r.RespondUnfurled(ctx, fmt.Sprintf("<https://xkcd.com/%d/>", comicID))
	})
}

// LinkToGoDoc responds with to messages with matchPrefix replaced by urlPrefix.
func LinkToGoDoc(matchPrefix, urlPrefix string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !HasPrefix(m, []string{matchPrefix}) {
			return
		}

		link := strings.TrimPrefix(m.Event.Text, matchPrefix)
		if i := strings.Index(link, " "); i > -1 {
			link = link[:i]
		}
		r.Respond(ctx, `<`+urlPrefix+link+`>`)
	})
}
