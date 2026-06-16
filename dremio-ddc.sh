#!/bin/bash

DDC_OUTPUT_FILE=/data/diag-$(date '+%Y%m%d-%H%M%S').tgz
# set -e
cd /data
/apps/bin/ddc collect k8s standard --queries-json-num-days $DREMIO_QUERY_NUMBER_DAYS --queries-perf-num-days $DREMIO_QUERY_PERF_NUMBER_DAYS --server-logs-num-days $DREMIO_LOGS_NUMBER_DAYS --namespace $DREMIO_KUBERNETES_NAMESPACE  --output-file $DDC_OUTPUT_FILE
azcopy cp --check-length=false "$DDC_OUTPUT_FILE" "$DREMIO_DROPZONE_URL"
