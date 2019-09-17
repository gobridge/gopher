package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gobridge/gopher/bot"
)

// Songs inspects the message for Spotify, Soundcloud, Tidal links.
func Songs() bot.Handler {
	var (
		regexSongNolink = regexp.MustCompile(`(?i)(nolink|song\.link)`)
		regexSongLink   = regexp.MustCompile(`(?i)(?:https?://)?(open\.spotify\.com/|spotify:|soundcloud\.com/|tidal\.com/)[^>\s]+`)
	)
	return bot.HandlerFunc(func(ctx context.Context, m bot.Message, r bot.Responder) {
		if regexSongNolink.MatchString(m.Event.Text) || !regexSongLink.MatchString(m.Event.Text) {
			return
		}

		var out string
		links := regexSongLink.FindAllString(m.Event.Text, -1)
		for _, link := range links {
			out = fmt.Sprintf("%s\n<https://song.link/%s>", out, link)
		}
		out = strings.TrimSpace(out)

		r.Respond(ctx, out)
	})
}
