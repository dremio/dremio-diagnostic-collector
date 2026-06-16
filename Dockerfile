FROM alpine/k8s:1.36.2
LABEL maintainer="Carsten Hufe <carsten.hufe@dremio.com>"

ARG TARGETARCH
ARG DDC_VERSION=v4.0.1

ENV HOME="/" \
    OS_NAME="linux" \
    DREMIO_KUBERNETES_NAMESPACE="default" \
    DREMIO_DROPZONE_URL="NOTSET" \
    DREMIO_QUERY_NUMBER_DAYS="30" \
    DREMIO_QUERY_PERF_NUMBER_DAYS="30" \
    DREMIO_LOGS_NUMBER_DAYS="1"

# Dependencies for azcopy (glibc-linked binary on musl-based Alpine)
RUN apk --update add --no-cache libc6-compat ca-certificates

# Azure CLI - azcopy
RUN if [ "$TARGETARCH" = "amd64" ]; then \
      wget -O azcopy.tar.gz https://aka.ms/downloadazcopy-v10-linux; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
      wget -O azcopy.tar.gz https://aka.ms/downloadazcopy-v10-linux-arm64; \
    fi && \
    tar -xvf azcopy.tar.gz && \
    cp ./azcopy_linux_*/azcopy /usr/bin/ && \
    rm -f azcopy.tar.gz && \
    rm -rf ./azcopy_linux_*

# DDC
RUN wget https://github.com/dremio/dremio-diagnostic-collector/releases/download/${DDC_VERSION}/ddc-linux-${TARGETARCH}.zip
RUN unzip ddc-linux-${TARGETARCH}.zip
RUN rm -f ddc-linux-${TARGETARCH}.zip
COPY dremio-ddc.sh /apps/bin/
RUN chmod +x /apps/bin/dremio-ddc.sh

CMD [ "/apps/bin/dremio-ddc.sh" ]
