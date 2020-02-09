package handlers

import (
	"context"
	"fmt"

	"github.com/gobridge/gopher/bot"
	"github.com/nlopes/slack"
)

// Join direct messages new users with a message about the Slack team and some recommended channels.
func Join(welcomeChannels []Channel) bot.JoinHandler {
	welcome := welcomeMessage(welcomeChannels)
	return bot.JoinHandlerFunc(func(ctx context.Context, event *slack.TeamJoinEvent, r bot.JoinResponder) {
		msg := "Hello " + event.User.Name + ",\n\n\n" + welcome

		r.RespondPrivate(ctx, msg)
	})
}

func welcomeMessage(channels []Channel) string {
	var welcomeChannels string
	for _, c := range channels {
		welcomeChannels += fmt.Sprintf("- #%s -> %s\n", c.Name, c.Description)
	}

	return `Welcome to the Gophers Slack channel.
This Slack is meant to connect gophers from all over the world in a central place.
There is also a forum: https://forum.golangbridge.org, you might want to check it out as well.
We have a few rules that you can see here: http://coc.golangbridge.org.

To share code, you should use: https://play.golang.org/ as it makes it easy for others to help you.

Out of respect for the visually impaired, please refrain from using Slack threads, as they are not accessible for screen readers. Instead, post your replies within the channel.

Here's a list of a few channels you could join:
` + welcomeChannels + `

If you want more suggestions, type "recommended channels".
There are quite a few other channels, depending on your interests or location (we have city / country wide channels).
Just click on the channel list and search for anything that crosses your mind.

If you are new to Go and want a copy of the Go In Action book, https://www.manning.com/books/go-in-action, please send an email to @wkennedy at bill@ardanlabs.com

If you are interested in a free copy of the Go Web Programming book by Sau Sheong Chang, @sausheong, please send him an email at sausheong@gmail.com

In case you want to customize your profile picture, you can use https://gopherize.me/ to create a custom gopher.

Final thing, #general might be too chatty at times but don't be shy to ask your Go related question.


Now, enjoy the community and have fun.`
}
