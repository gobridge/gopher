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