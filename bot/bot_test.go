package bot

import (
	"testing"

	"github.com/nlopes/slack"
)

func TestSongLink(t *testing.T) {
	testMsg := func(text string) *slack.MessageEvent {
		return &slack.MessageEvent{
			Msg: slack.Msg{
				Text:            text,
				Timestamp:       "1000",
				ThreadTimestamp: "1200",
			},
		}
	}

	t.Run("skips https://google.com/a", func(t *testing.T) {
		in := testMsg("https://google.com/a")
		expected := ""
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("skips https://song.link/https://open.spotify.com/track", func(t *testing.T) {
		in := testMsg("<https://song.link/https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>")
		expected := ""
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("skips https://SONG.LINK/https://open.spotify.com/track", func(t *testing.T) {
		in := testMsg("<https://SONG.LINK/https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>")
		expected := ""
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("skips nolink https://open.spotify.com/track", func(t *testing.T) {
		in := testMsg("noLink <https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>")
		expected := ""
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("ignores `Wait, does Slack recognize the Spotify URIs now?`", func(t *testing.T) {
		in := testMsg("Wait, does Slack recognize the Spotify URIs now?")
		expected := ""
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("detects http://open.spotify.com/a", func(t *testing.T) {
		in := testMsg("[drum and bass, uk hardcore, jazz, gospel] https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ")
		expected := "<https://song.link/https://open.spotify.com/track/0nypsuS2jtogLaJDcRQ4Ya?si=wqgnW8jeS9aYEqWagrDadQ>"
		msg, params := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
		if params.ThreadTimestamp != in.ThreadTimestamp {
			t.Errorf("expected output to follow OP's thread, got %s", params.ThreadTimestamp)
		}
		if params.UnfurlMedia == true || params.UnfurlLinks == true {
			t.Error("expected output to collapse everything")
		}
	})

	t.Run("detects spotify:album:54OM8icMyeUMzhpJn8Igmk", func(t *testing.T) {
		in := testMsg("spotify:album:54OM8icMyeUMzhpJn8Igmk")
		expected := "<https://song.link/spotify:album:54OM8icMyeUMzhpJn8Igmk>"
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("detects https://soundcloud.com/a/b", func(t *testing.T) {
		in := testMsg("check out https://soundcloud.com/lematos/highway-64-1")
		expected := "<https://song.link/https://soundcloud.com/lematos/highway-64-1>"
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("detects https://tidal.com/a/1", func(t *testing.T) {
		in := testMsg("https://tidal.com/album/86024647")
		expected := "<https://song.link/https://tidal.com/album/86024647>"
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

	t.Run("handles multiple links", func(t *testing.T) {
		in := testMsg("check out https://soundcloud.com/kennedyjones/gramatikkennedyjones and https://open.spotify.com/track/5Fwq7F2yjF3eQnvkfjN4LY fellow gophers")
		expected := `<https://song.link/https://soundcloud.com/kennedyjones/gramatikkennedyjones>
<https://song.link/https://open.spotify.com/track/5Fwq7F2yjF3eQnvkfjN4LY>`
		msg, _ := songlink(in)
		if msg != expected {
			t.Errorf("expected: %q\nactual:%q", expected, msg)
		}
	})

}
