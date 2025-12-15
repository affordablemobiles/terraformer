FROM golang:1.25 as build-env

ADD . /go/src/terraformer
WORKDIR /go/src/terraformer

ARG CGO_ENABLED=0

RUN go build -v -tags google,single_provider -ldflags "-s -w" -o /go/bin/terraformer

FROM debian:trixie

RUN apt-get update && \
    apt-get dist-upgrade -y && \
    apt-get install -y wget curl rsync git gnupg software-properties-common apt-transport-https && \
    wget -O- https://apt.releases.hashicorp.com/gpg | \ 
        gpg --dearmor | \ 
        tee /usr/share/keyrings/hashicorp-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(grep -oP '(?<=UBUNTU_CODENAME=).*' /etc/os-release || lsb_release -cs) main" | \
        tee /etc/apt/sources.list.d/hashicorp.list && \
    curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | \
        gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg && \
    echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | \
        tee -a /etc/apt/sources.list.d/google-cloud-sdk.list && \
    apt-get update && \
    apt-get install -y terraform google-cloud-cli google-cloud-cli-config-connector && \
    apt-get clean

ADD ./assets/terraform-template /opt/terraform-template

RUN cd /opt/terraform-template && \
    terraform init

COPY --from=build-env /go/bin/terraformer /usr/local/bin/
