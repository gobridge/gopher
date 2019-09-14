package bot

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/trace"
	"github.com/nlopes/slack"
)

// Logger function
type Logger func(message string, args ...interface{})

// A Handler responds to a message.
type Handler interface {
	Handle(context.Context, Message, Responder)
}

// HandlerFunc adapts a function to be a Handler.
type HandlerFunc func(context.Context, Message, Responder)

// Handle calls f(ctx, m, r).
func (f HandlerFunc) Handle(ctx context.Context, m Message, r Responder) {
	f(ctx, m, r)
}

// Message contains the slack.MessageEvent and processed information.
type Message struct {
	Event         *slack.MessageEvent // OriginalEvent
	TrimmedText   string              // Event.Text with whitespace and bot mention removed.
	DirectedToBot bool                // True if @mention to bot or DM to bot.
}

// Responder provides methods for responding to messages.
type Responder interface {
	Respond(ctx context.Context, msg string)
	RespondUnfurled(ctx context.Context, msg string)
	RespondWithAttachment(ctx context.Context, msg, attachment string)
	RespondPrivate(ctx context.Context, msg string)
	RespondPrivateWithAttachment(ctx context.Context, msg, attachment string)
	React(ctx context.Context, reaction string)
}

// A JoinHandler responds to a user joining the Slack team.
type JoinHandler interface {
	Handle(ctx context.Context, event *slack.TeamJoinEvent, r JoinResponder)
}

// JoinHandlerFunc adapts a function to be a JoinHandler.
type JoinHandlerFunc func(ctx context.Context, event *slack.TeamJoinEvent, r JoinResponder)

// Handle calls jh(ctx, event, r).
func (jh JoinHandlerFunc) Handle(ctx context.Context, event *slack.TeamJoinEvent, r JoinResponder) {
	jh(ctx, event, r)
}

// JoinResponder provides methods for responding to a user joing the Slack team.
type JoinResponder interface {
	RespondPrivate(ctx context.Context, msg string)
}

// Bot structure
type Bot struct {
	devMode     bool
	logf        Logger
	slack       *slack.Client
	trace       *trace.Client
	handler     Handler
	joinHandler JoinHandler

	msgprefix string
	id        string
	name      string
}

// New will create a new Bot.
func New(sc *slack.Client, tc *trace.Client, devMode bool, log Logger, h Handler, jh JoinHandler) *Bot {
	return &Bot{
		devMode:     devMode,
		logf:        log,
		slack:       sc,
		trace:       tc,
		handler:     h,
		joinHandler: jh,
	}
}

// Init must be called before anything else in order to initialize the bot
func (b *Bot) Init(ctx context.Context) error {
	span := trace.FromContext(ctx).NewChild("Bot.Init")
	defer span.Finish()

	b.logf("Determining bot ID")

	ai, err := b.slack.AuthTestContext(ctx)
	if err != nil {
		return fmt.Errorf("retrieving bot user info: %v", err)
	}

	b.id = ai.UserID
	b.name = ai.User
	b.msgprefix = strings.ToLower("<@" + b.id + ">")

	b.logf("Initialized %s with ID (%q) and msgprefix (%q) \n", b.name, b.id, b.msgprefix)

	go b.handleEvents()

	return nil
}

func (b *Bot) handleEvents() {
	rtm := b.slack.NewRTM()
	go rtm.ManageConnection()

	for msg := range rtm.IncomingEvents {
		switch message := msg.Data.(type) {
		case *slack.MessageEvent:
			go b.handleMessage(message)

		case *slack.TeamJoinEvent:
			go b.handleTeamJoin(message)
		}
	}
}

// handleTeamJoin is called when the someone joins the team
func (b *Bot) handleTeamJoin(event *slack.TeamJoinEvent) {
	span := b.trace.NewSpan("Bot.TeamJoined")
	defer span.Finish()

	ctx := trace.NewContext(context.Background(), span)

	responder := joinResponder{b: b, event: event}
	b.joinHandler.Handle(ctx, event, responder)
}

// handleMessage will process the incoming message and respond appropriately
func (b *Bot) handleMessage(event *slack.MessageEvent) {
	if event.BotID != "" || event.User == "" || event.SubType == "bot_message" {
		return
	}

	span := b.trace.NewSpan("Bot.HandleMessage")
	span.SetLabel("eventText", event.Text)
	defer span.Finish()

	trimmedText := strings.TrimSpace(strings.ToLower(event.Text))

	isBotMessage := b.isBotMessage(event, trimmedText)
	if isBotMessage {
		trimmedText = b.trimBot(trimmedText)
		trimmedText = strings.TrimSpace(trimmedText)
	}

	if b.devMode {
		b.logf("%#v\n", *event)
		b.logf("got message: %s\n", event.Text)
		b.logf("isBotMessage: %t\n", isBotMessage)
		b.logf("channel: %s -> message: %q\n", event.Channel, trimmedText)
	}

	ctx := trace.NewContext(context.Background(), span)
	m := Message{
		Event:         event,
		TrimmedText:   trimmedText,
		DirectedToBot: isBotMessage,
	}
	r := responder{
		bot:   b,
		event: event,
	}

	b.handler.Handle(ctx, m, r)
}

func (b *Bot) isBotMessage(event *slack.MessageEvent, eventText string) bool {
	return strings.HasPrefix(eventText, b.msgprefix) ||
		strings.HasPrefix(eventText, "gopher") || // emoji :gopher: or text `gopher`
		strings.HasPrefix(event.Channel, "D") // direct message
}

func (b *Bot) trimBot(msg string) string {
	msg = strings.Replace(msg, strings.ToLower(b.msgprefix), "", 1)
	msg = strings.TrimPrefix(msg, "gopher")
	msg = strings.Trim(msg, " :\n")

	return msg
}

// PostMessage sends messages to a Slack channel.
//
// Links and media is not unfurled.
func (b *Bot) PostMessage(ctx context.Context, channel, text string, opts ...slack.MsgOption) error {
	opts = append(opts,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
		slack.MsgOptionPostMessageParameters(slack.PostMessageParameters{LinkNames: 1}),
		slack.MsgOptionText(text, false),
	)
	_, _, err := b.slack.PostMessageContext(ctx, channel, opts...)
	return err
}

type responder struct {
	bot   *Bot
	event *slack.MessageEvent
}

func (r responder) Respond(ctx context.Context, msg string) {
	if r.bot.devMode {
		r.bot.logf("should reply to message %s with %s\n", r.event.Text, msg)
	}
	err := r.bot.PostMessage(ctx, r.event.Channel, msg,
		slack.MsgOptionTS(r.event.ThreadTimestamp),
	)
	if err != nil {
		r.bot.logf("%s\n", err)
	}
}

func (r responder) RespondUnfurled(ctx context.Context, msg string) {
	if r.bot.devMode {
		r.bot.logf("should reply to message %s with %s\n", r.event.Text, msg)
	}
	_, _, err := r.bot.slack.PostMessageContext(ctx, r.event.Channel,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionTS(r.event.ThreadTimestamp),
		slack.MsgOptionEnableLinkUnfurl(),
		slack.MsgOptionText(msg, false),
	)
	if err != nil {
		r.bot.logf("%s\n", err)
	}
}

func (r responder) RespondWithAttachment(ctx context.Context, msg, attachement string) {
	r.bot.PostMessage(ctx, r.event.Channel, msg,
		slack.MsgOptionAttachments(slack.Attachment{Text: attachement}),
	)
}

func (r responder) RespondPrivate(ctx context.Context, msg string) {
	r.bot.PostMessage(ctx, r.event.User, msg)
}

func (r responder) RespondPrivateWithAttachment(ctx context.Context, msg, attachement string) {
	r.bot.PostMessage(ctx, r.event.User, msg,
		slack.MsgOptionAttachments(slack.Attachment{Text: attachement}),
	)
}

func (r responder) React(ctx context.Context, reaction string) {
	if r.bot.devMode {
		r.bot.logf("should reply to message %s with %s\n", r.event.Text, reaction)
	}
	item := slack.ItemRef{
		Channel:   r.event.Channel,
		Timestamp: r.event.Timestamp,
	}
	err := r.bot.slack.AddReactionContext(ctx, reaction, item)
	if err != nil {
		r.bot.logf("%s\n", err)
		return
	}
}

type joinResponder struct {
	b     *Bot
	event *slack.TeamJoinEvent
}

func (r joinResponder) RespondPrivate(ctx context.Context, msg string) {
	err := r.b.PostMessage(ctx, r.event.User.ID, msg)
	if err != nil {
		r.b.logf("%s\n", err)
	}
}
