#!/usr/bin/env bash

export GOOGLE_APPLICATION_CREDENTIALS=${HOME}/account-auth.json

echo ${GCLOUD_SERVICE_KEY} | base64 --decode -i > ${HOME}/account-auth.json
/opt/google-cloud-sdk/bin/gcloud auth activate-service-account --key-file=${HOME}/account-auth.json

/opt/google-cloud-sdk/bin/gcloud -q config set project ${PROJECT_NAME}
/opt/google-cloud-sdk/bin/gcloud -q config set container/cluster ${CLUSTER_NAME}
/opt/google-cloud-sdk/bin/gcloud -q config set compute/zone ${CLOUDSDK_COMPUTE_ZONE}
/opt/google-cloud-sdk/bin/gcloud -q container clusters get-credentials ${CLUSTER_NAME}

/opt/google-cloud-sdk/bin/gcloud docker -- push eu.gcr.io/${PROJECT_NAME}/bot
/opt/google-cloud-sdk/bin/kubectl --namespace=gopher-slack-bot set image deployment/gopher-slack-bot gopher-slack-bot=eu.gcr.io/gophers-slack-bot/bot:${CIRCLE_SHA1}
