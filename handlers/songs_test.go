package handlers

import (
	"context"
	"testing"

	"github.com/gobridge/gopher/bot"
	"github.com/nlopes/slack"
)

type testResponder struct {
	bot.Responder
	msg string
}

func (tr *testResponder) Respond(ctx context.Context, msg string) {
	tr.msg = msg
}

func TestSongLink(t *testing.T) {
	songlink := func(text string) string {
		event := &slack.MessageEvent{
			Msg: slack.Msg{
				Text:            text,
				Timestamp:       "1000",
				ThreadTimestamp: "1200",
			},
		}
		var tr testResponder
		Songs().Handle(context.Background(), bot.Message{Event: event}, &tr)
		return tr.msg
	}

	t.Run("skips https://google.com/a", func(t *testing.T) {
		expected := ""
		msg := songlink("https://google.com/a")
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("skips https://song.link/https://open.spotify.com/track", func(t *testing.T) {
		msg := songlink("<https://song.link/https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>")
		expected := ""
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("skips https://SONG.LINK/https://open.spotify.com/track", func(t *testing.T) {
		msg := songlink("<https://SONG.LINK/https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>")
		expected := ""
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("skips nolink https://open.spotify.com/track", func(t *testing.T) {
		msg := songlink("noLink <https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>")
		expected := ""
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("ignores `Wait, does Slack recognize the Spotify URIs now?`", func(t *testing.T) {
		msg := songlink("Wait, does Slack recognize the Spotify URIs now?")
		expected := ""
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("detects http://open.spotify.com/a", func(t *testing.T) {
		msg := songlink("[drum and bass, uk hardcore, jazz, gospel] https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ")
		expected := "<https://song.link/https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>"
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("detects spotify:album:54OM8icMyeUMzhpJn8Igmk", func(t *testing.T) {
		msg := songlink("spotify:album:54OM8icMyeUMzhpJn8Igmk")
		expected := "<https://song.link/spotify:album:54OM8icMyeUMzhpJn8Igmk>"
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("detects https://soundcloud.com/a/b", func(t *testing.T) {
		msg := songlink("check out https://soundcloud.com/lematos/highway-64-1")
		expected := "<https://song.link/https://soundcloud.com/lematos/highway-64-1>"
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("detects https://tidal.com/a/1", func(t *testing.T) {
		msg := songlink("https://tidal.com/album/86024647")
		expected := "<https://song.link/https://tidal.com/album/86024647>"
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("handles multiple links", func(t *testing.T) {
		msg := songlink("check out https://soundcloud.com/kennedyjones/gramatikkennedyjones and https://open.spotify.com/track/5Fwq7F2yjF3eQnvkfjN4LY fellow gophers")
		expected := `<https://song.link/https://soundcloud.com/kennedyjones/gramatikkennedyjones>
<https://song.link/https://open.spotify.com/track/5Fwq7F2yjF3eQnvkfjN4LY>`
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

}
