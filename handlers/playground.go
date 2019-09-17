package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/gobridge/gopher/bot"
	"github.com/nlopes/slack"
)

type playground struct {
	http     *http.Client
	slack    *slack.Client
	logf     bot.Logger
	minLines int
}

// SuggestPlayground uploads messages/files to the playground when they have at least minLines or
// has files that are of type "go" or "text".
//
// After uploading, a link will be posted to the channel and a suggestion to use the playground is
// sent directly to the user.
func SuggestPlayground(h *http.Client, s *slack.Client, l bot.Logger, minLines int) bot.Handler {
	return playground{
		http:     h,
		slack:    s,
		logf:     l,
		minLines: minLines,
	}
}

// TODO: logic can have a lot of false positives and code is unlikely to be formatted correctly in playground
func (p playground) Handle(ctx context.Context, m bot.Message, r bot.Responder) {
	if strings.Contains(m.Event.Text, "nolink") || (!m.Event.Upload && strings.Count(m.Event.Text, "\n") < p.minLines) {
		return
	}

	// uploads
	for _, file := range m.Event.Files {
		if file.Filetype == "go" || file.Filetype == "text" {
			p.suggestPlaygroundUploads(ctx, m, r)
			return
		}
	}

	// message
	p.suggestPlaygroundPost(ctx, m, r)
}

func (p playground) suggestPlaygroundUploads(ctx context.Context, m bot.Message, r bot.Responder) {
	if len(m.Event.Files) == 0 {
		return
	}

	// Empirically, attempting to call GetFileInfoContext too quickly after a
	// file is uploaded can cause a "file_not_found" error.
	time.Sleep(1 * time.Second)

	for _, file := range m.Event.Files {
		info, _, _, err := p.slack.GetFileInfoContext(ctx, file.ID, 0, 0)
		if err != nil {
			p.logf("error while getting file info for ID %q: %v", file.ID, err)
			return
		}

		if info.Lines < 6 || info.PrettyType == "Plain Text" {
			return
		}

		var buf bytes.Buffer
		err = p.slack.GetFile(info.URLPrivateDownload, &buf)
		if err != nil {
			p.logf("error while fetching the file %v\n", err)
			return
		}

		link, err := p.postToPlayground(ctx, &buf)
		if err != nil {
			p.logf("%s\n", err)
			return
		}

		r.Respond(ctx, `The above code in playground: <`+link+`>`)
	}

	r.RespondPrivate(ctx,
		`Hello. I've noticed you uploaded a Go file. To facilitate collaboration and make `+
			`this easier for others to share back the snippet, please consider using: `+
			`<https://play.golang.org>. If you wish to not link against the playground, please use `+
			`"nolink" in the message. Thank you.`,
	)
}

func (p playground) suggestPlaygroundPost(ctx context.Context, m bot.Message, r bot.Responder) {
	link, err := p.postToPlayground(ctx, strings.NewReader(m.Event.Text))
	if err != nil {
		p.logf("%s\n", err)
		return
	}

	r.Respond(ctx, `The above code in playground: <`+string(link)+`>`)
	r.RespondPrivate(ctx, `Hello. I've noticed you've written a large block of text (more than 9 lines). `+
		`To make the conversation easier to follow the conversation and facilitate collaboration, `+
		`please consider using: <https://play.golang.org> if you shared code. If you wish to not `+
		`link against the playground, please start the message with "nolink". Thank you.`,
	)
}

func (p playground) postToPlayground(ctx context.Context, body io.Reader) (link string, err error) {
	req, err := http.NewRequest("POST", "https://play.golang.org/share", body)
	if err != nil {
		return "", err
	}
	req = req.WithContext(ctx)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("User-Agent", "Gophers Slack bot")

	resp, err := p.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("got non-200 response: %v", resp.StatusCode)
	}

	linkID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return `https://play.golang.org/p/` + string(linkID), nil
}
