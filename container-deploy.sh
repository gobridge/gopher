#!/usr/bin/env bash

set -ex

export GOOGLE_APPLICATION_CREDENTIALS=${HOME}/account-auth.json

sudo /opt/google-cloud-sdk/bin/gcloud docker push eu.gcr.io/${PROJECT_NAME}/bot
sudo chown -R ubuntu:ubuntu /home/ubuntu/.kube
kubectl rolling-update --namespace=gopher-slack-bot --image=eu.gcr.io/gopher-slack-bot/bot:${CIRCLE_SHA1}
