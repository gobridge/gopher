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
		if m.DirectedToBot {
			h.Handle(ctx, m, r)
		}
	})
}

// RespondTo responds to messages when Message.TrimmedText is in prompts.
func RespondTo(prompts []string, response string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		for _, prompt := range prompts {
			if m.TrimmedText == prompt {
				r.Respond(ctx, response)
				return
			}
		}
	})
}

// ReactWhenContains adds reactions to messages that contain s.
func ReactWhenContains(s string, reactions ...string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !strings.Contains(m.Event.Text, s) {
			return
		}

		for _, reaction := range reactions {
			r.React(ctx, reaction)
		}
	})
}

// ReactWhenHasPrefix addes reactions to messages when Message.TrimmedText has prefix s.
func ReactWhenHasPrefix(s string, reactions ...string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if !strings.HasPrefix(m.TrimmedText, s) {
			return
		}

		for _, reaction := range reactions {
			r.React(ctx, reaction)
		}
	})
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
		for _, prompt := range prompts {
			if m.TrimmedText == prompt {
				r.Respond(ctx, msg)
				return
			}
		}
	})
}

// BotVersion responds to messages with the bot's version when Message.TrimmedText
// matches prompt.
func BotVersion(prompt, version string) bot.Handler {
	msg := "My version is: " + version
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if m.TrimmedText != prompt {
			return
		}
		r.Respond(ctx, msg)
	})
}

// CoinFlip responds to messages with "heads" or "tails" when Message.TrimmedText
// matches one of prompts.
func CoinFlip(prompts []string) bot.Handler {
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		for _, prompt := range prompts {
			if m.TrimmedText == prompt {
				if rand.Intn(2) == 0 {
					r.Respond(ctx, "heads")
				} else {
					r.Respond(ctx, "tails")
				}
				return
			}
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
		if m.TrimmedText != prompt {
			return
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
		if !strings.HasPrefix(m.TrimmedText, prefix) {
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
		if !strings.HasPrefix(m.TrimmedText, prefix) {
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
		if !strings.HasPrefix(m.Event.Text, matchPrefix) {
			return
		}

		link := strings.TrimPrefix(m.Event.Text, matchPrefix)
		if i := strings.Index(link, " "); i > -1 {
			link = link[:i]
		}
		r.Respond(ctx, `<`+urlPrefix+link+`>`)
	})
}
