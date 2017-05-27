#!/usr/bin/env bash

export GOOGLE_APPLICATION_CREDENTIALS=${HOME}/account-auth.json

sudo /opt/google-cloud-sdk/bin/gcloud docker -- push eu.gcr.io/${PROJECT_NAME}/bot
sudo chown -R ubuntu:ubuntu /home/ubuntu/.kube
kubectl --namespace=gopher-slack-bot set image deployment/gopher-slack-bot gopher-slack-bot=eu.gcr.io/gophers-slack-bot/bot:${CIRCLE_SHA1}
