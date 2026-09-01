FROM registry.access.redhat.com/ubi10/go-toolset@sha256:81f0a8604f87b126a077cd55055fd5cc2b7b6536b3176171001b4dd47c322dfd AS go-builder
ARG TARGETARCH

ENV GOTOOLCHAIN=auto
WORKDIR /workspace

COPY go.mod go.sum ./
RUN if [ -f /cachi2/cachi2.env ]; then . /cachi2/cachi2.env; fi && go mod download

COPY cmd/helm-chart-oci/ cmd/helm-chart-oci/
RUN if [ -f /cachi2/cachi2.env ]; then . /cachi2/cachi2.env; fi && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -o /tmp/helm-chart-oci ./cmd/helm-chart-oci

ARG TARGETPLATFORM
FROM --platform=$TARGETPLATFORM quay.io/konflux-ci/task-runner:1.6.0@sha256:1abfe4e50d4e961d0fd9790202565f93ee650fe8dfc50932c94989acba10485f AS task-runner-oc

FROM registry.access.redhat.com/ubi10/ubi@sha256:4690398669a07627339936c9e79b05233053056ce688efeb4400d3c1c530486b

LABEL \
    name="konflux-ci/tools" \
    description="Tools for Red Hat AppStudio" \
    io.k8s.description="Tools for Red Hat AppStudio" \
    io.k8s.display-name="Tools for Red Hat AppStudio" \
    io.openshift.tags="appstudio" \
    summary="This image contains various tools that are used within Red Hat \
AppStudio. The included tools are, for the most part, written in Python." \
    com.redhat.component="konflux-ci-tools-container" \
    version="1.0" \
    release="1" \
    vendor="Red Hat, Inc." \
    distribution-scope="public" \
    url="https://github.com/konflux-ci/tools"

ENV REQUESTS_CA_BUNDLE=/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem

USER 0
ADD . /tmp/src
ADD --chown=root:root --chmod=644 data/ca-trust/* /etc/pki/ca-trust/source/anchors
RUN update-ca-trust

RUN if [ -f /cachi2/cachi2.env ]; then . /cachi2/cachi2.env; fi && \
    yum install -y skopeo python3-pip && yum clean all

COPY --from=task-runner-oc /usr/local/bin/oc /usr/local/bin/oc
COPY --from=go-builder /tmp/helm-chart-oci /usr/local/bin/helm-chart-oci

RUN if [ -f /cachi2/cachi2.env ]; then . /cachi2/cachi2.env; fi && \
    pip install --no-cache-dir -r /tmp/src/deps/pip/requirements.txt && \
    pip install --no-cache-dir /tmp/src

RUN useradd -u 1001 -g 0 -l -M -d /usr/local/bin -s /sbin/nologin default

USER 1001
