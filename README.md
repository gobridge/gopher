# Gopher

[![GoDoc](https://godoc.org/github.com/gobridge/gopher?status.svg)](https://godoc.org/github.com/gobridge/gopher)
[![Go Report Card](https://goreportcard.com/badge/github.com/gobridge/gopher)](https://goreportcard.com/report/github.com/gobridge/gopher)

This is the Slack bot for the Gophers Slack.

You can get an invite from [here](https://invite.slack.golangbridge.org/)

## Running

To run this you need to set the the following environment variables:

* `GOPHERS_SLACK_BOT_TOKEN` - the Slack bot token
* `GOPHERS_SLACK_BOT_NAME` - the Slack bot name
* `GOPHERS_SLACK_BOT_DEV_MODE` - boolean, set the bot in development mode

## OLD Instructions

Note: Not sure any of the stuff below here works anymore.

## Kubernetes

To get the bot running in Kubernetes you need to run the following commands:

```console
$ gcloud container clusters create gopher-slack-bot \
    --zone europe-west1-c \
    --additional-zones=europe-west1-d,europe-west1-b \
    --num-nodes=1 \
    --local-ssd-count=0 \
    --machine-type=f1-micro \
    --disk-size=10

$ kubectl create namespace gopher-slack-bot

$ cp secrets.yaml.template secrets.yaml
$ echo `echo -n 'slackTokenHere' | base64` >> secrets.yaml
$ kubectl create -f ./secrets.yaml --namespace=gopher-slack-bot

$ cp secrets-datastore.yaml.template secrets-datastore.yaml
$ echo `cat datastore.json | base64 -w 0` >> secrets-datastore.yaml
$ kubectl create -f ./secrets-datastore.yaml --namespace=gopher-slack-bot

$ kubectl create -f ./deployment.yaml --namespace=gopher-slack-bot
```

## Development

To run gometalinter

```console
$ go get -v -u github.com/alecthomas/gometalinter
...
$ gometalinter ./... --deadline=20s --vendor --sort=linter --disable=gotype
...
```

## License

This software is created under Apache v2 License. For the full license text, please see License.md