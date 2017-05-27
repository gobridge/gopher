#!/usr/bin/env bash

pip install pyopenssl
apt-get install python-openssl python3-openssl
/opt/google-cloud-sdk/bin/gcloud -q components update
/opt/google-cloud-sdk/bin/gcloud -q components update kubectl
$GCLOUD_SERVICE_KEY | base64 --decode -i > ${HOME}/account-auth.json
/opt/google-cloud-sdk/bin/gcloud auth activate-service-account --key-file ${HOME}/account-auth.json
/opt/google-cloud-sdk/bin/gcloud -q config set project ${PROJECT_NAME}
/opt/google-cloud-sdk/bin/gcloud -q config set container/cluster ${CLUSTER_NAME}
/opt/google-cloud-sdk/bin/gcloud -q config set compute/zone ${CLOUDSDK_COMPUTE_ZONE}
/opt/google-cloud-sdk/bin/gcloud -q container clusters get-credentials $CLUSTER_NAME