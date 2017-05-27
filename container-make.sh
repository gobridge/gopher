#!/usr/bin/env bash

sudo pip install pyopenssl
sudo apt-get install python-openssl python3-openssl
sudo /opt/google-cloud-sdk/bin/gcloud -q components update
sudo /opt/google-cloud-sdk/bin/gcloud -q components update kubectl
sudo $GCLOUD_SERVICE_KEY | base64 --decode -i > ${HOME}/account-auth.json
sudo /opt/google-cloud-sdk/bin/gcloud auth activate-service-account --key-file ${HOME}/account-auth.json
sudo /opt/google-cloud-sdk/bin/gcloud -q config set project ${PROJECT_NAME}
sudo /opt/google-cloud-sdk/bin/gcloud -q config set container/cluster ${CLUSTER_NAME}
sudo /opt/google-cloud-sdk/bin/gcloud -q config set compute/zone ${CLOUDSDK_COMPUTE_ZONE}
sudo /opt/google-cloud-sdk/bin/gcloud -q container clusters get-credentials $CLUSTER_NAME


CONTAINER_NAME=eu.gcr.io/${PROJECT_NAME}/bot
CONTAINER_TAG=${CIRCLE_SHA1}

PROJECT_NAME='github.com/gopheracademy/gopher'
PROJECT_DIR=${PWD}

CONTAINER_GOPATH='/go'
CONTAINER_PROJECT_DIR="${CONTAINER_GOPATH}/src/${PROJECT_NAME}"

docker run --rm \
        --net="host" \
        -v ${PROJECT_DIR}:${CONTAINER_PROJECT_DIR} \
        -e GOPATH=${CONTAINER_GOPATH} \
        -e CGO_ENABLED=0 \
        -w "${CONTAINER_PROJECT_DIR}" \
        golang:1.8.3-alpine \
        go build -v -tags netgo -installsuffix netgo -ldflags "-X main.botVersion=${CONTAINER_TAG}" -o gopher ${PROJECT_NAME}

docker build -f ${PROJECT_DIR}/Dockerfile \
    -t ${CONTAINER_NAME}:${CONTAINER_TAG} \
    "${PROJECT_DIR}"

docker tag ${CONTAINER_NAME}:${CONTAINER_TAG} ${CONTAINER_NAME}:latest

rm -f "${PROJECT_DIR}/gopher"
