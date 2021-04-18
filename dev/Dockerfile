FROM ubuntu:20.04
WORKDIR /

RUN apt-get update && \
    apt-get install -y curl && \
    curl -LO https://dl.k8s.io/release/v1.21.0/bin/linux/amd64/kubectl && chmod +x kubectl && mv kubectl /usr/local/bin/ && \
    curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 && chmod 700 get_helm.sh && ./get_helm.sh

ADD elastic/helm-charts/ wcp-elk/
ADD bin/manager .

ENTRYPOINT ["/manager"]
