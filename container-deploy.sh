#!/usr/bin/env bash

export PATH=${PATH}:/opt/google-cloud-sdk/bin
export GOOGLE_APPLICATION_CREDENTIALS=${HOME}/account-auth.json
export KUBECONFIG=${HOME}/kubeconfig

echo ${GCLOUD_SERVICE_KEY} | base64 --decode -i > ${GOOGLE_APPLICATION_CREDENTIALS}
echo ${RAW_KUBECONFIG} | base64 --decode -i > ${HOME}/kubeconfig
gcloud auth activate-service-account --key-file=${GOOGLE_APPLICATION_CREDENTIALS}

gcloud -q config set project ${PROJECT_NAME}
gcloud -q config set container/cluster ${CLUSTER_NAME}
gcloud -q config set container/use_client_certificate True
gcloud -q config set compute/zone europe-west1-d

gcloud docker -- push eu.gcr.io/${PROJECT_NAME}/bot

echo ${KUBE_CA_PEM} | base64 --decode -i > ${HOME}/kube_ca.pem
kubectl config set-cluster default-cluster --server=${KUBE_URL} --certificate-authority="${HOME}/kube_ca.pem"
kubectl config set-credentials default-admin --token=`echo ${RAW_KUBE_TOKEN} | base64 --decode -i`
kubectl config set-context default-system --cluster=default-cluster --user=default-admin --namespace default
kubectl config use-context default-system

kubectl --namespace=gopher-slack-bot set image deployment/gopher-slack-bot gopher-slack-bot=eu.gcr.io/gophers-slack-bot/bot:${CIRCLE_SHA1}

rm -f ${HOME}/kube_cap.pem ${HOME}/kubeconfig ${GOOGLE_APPLICATION_CREDENTIALS}
