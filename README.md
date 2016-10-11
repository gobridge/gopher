# Gopher

[![CircleCI](https://circleci.com/gh/gopheracademy/gopher.svg?style=svg)](https://circleci.com/gh/gopheracademy/gopher)
[![GoDoc](https://godoc.org/github.com/gopher/gopher?status.svg)](https://godoc.org/github.com/gopheracademy/gopher)
[![Go Report Card](https://goreportcard.com/badge/github.com/gopheracademy/gopher)](https://goreportcard.com/report/github.com/gopheracademy/gopher)

This is a Slack bot for the Gophers Slack.

You can get an invite from [here](https://invite.slack.golangbridge.org/)

## Running

To run this you need to set the the following environment variables:
- ` GOPHERS_SLACK_BOT_TOKEN ` - the Slack bot
token
- ` GOPHERS_SLACK_BOT_NAME ` - the Slack bot name (in development `tempbot` is used)
- ` GOPHERS_SLACK_BOT_DEV_MODE ` - boolean, set the bot in development mode

## Kubernetes

To get the bot running in Kubernetes you need to run the following commands:

```
gcloud container clusters create gopher-slack-bot \
    --zone europe-west1-c \
    --additional-zones=europe-west1-d,europe-west1-b \
    --num-nodes=1 \
    --local-ssd-count=0 \
    --machine-type=f1-micro \
    --disk-size=10

kubectl create namespace gopher-slack-bot

cp secrets.yaml.template secrets.yaml
echo `echo -n 'slackTokenHere' | base64` >> secrets.yaml
kubectl create -f ./secrets.yaml --namespace=gopher-slack-bot
kubectl create -f ./deployment.yaml --namespace=gopher-slack-bot
```

## License

This software is created under Apache v2 License. For the full license text, please see License.md