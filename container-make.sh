#!/usr/bin/env bash

set -ex

CONTAINER_NAME=eu.gcr.io/${PROJECT_NAME}/bot
CONTAINER_TAG=${CIRCLE_SHA1}

PROJECT_NAME='github.com/gopheracademy/gopher'
PROJECT_DIR=${PWD}

CONTAINER_GOPATH='/go'
CONTAINER_PROJECT_DIR="${CONTAINER_GOPATH}/src/${PROJECT_NAME}"
CONTAINER_PROJECT_GOPATH="${CONTAINER_GOPATH}"

docker run --rm \
        --net="host" \
        -v ${PROJECT_DIR}:${CONTAINER_PROJECT_DIR} \
        -e GOPATH=${CONTAINER_PROJECT_GOPATH} \
        -e CGO_ENABLED=0 \
        -w "${CONTAINER_PROJECT_DIR}" \
        golang:1.7.0-alpine \
        go build -v -tags netgo -installsuffix netgo -ldflags "-X main.botVersion=${CONTAINER_TAG}" -o gopher ${PROJECT_NAME}

docker build -f ${PROJECT_DIR}/Dockerfile \
    -t ${CONTAINER_NAME}:${CONTAINER_TAG} \
    "${PROJECT_DIR}"

docker tag ${CONTAINER_NAME}:${CONTAINER_TAG} ${CONTAINER_NAME}:latest

rm -f "${PROJECT_DIR}/gopher"
