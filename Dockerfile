FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./internal/ui/dist
ARG VERSION=dev
ARG IMAGE=
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.image=${IMAGE}" -o /out/myinfra ./cmd/myinfra

# The image carries every tool MyInfra drives, so that a copy moved to a
# server or a cluster installs machines like the one on a laptop: Ansible
# with its Python and the SSH client, and OpenTofu.
FROM debian:12-slim
ARG TARGETARCH
# Keep in step with tofuVersion in internal/tools/install.go.
ARG TOFU_VERSION=1.13.1
ARG ANSIBLE_VERSION=2.18.18
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates openssh-client python3 python3-venv curl unzip; \
    python3 -m venv /opt/ansible; \
    /opt/ansible/bin/pip install --no-cache-dir "ansible-core==${ANSIBLE_VERSION}"; \
    for tool in /opt/ansible/bin/ansible*; do ln -s "$tool" /usr/local/bin/; done; \
    curl -fsSL -o /tmp/tofu.zip "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_${TARGETARCH}.zip"; \
    curl -fsSL -o /tmp/tofu.sums "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_SHA256SUMS"; \
    (cd /tmp && grep " tofu_${TOFU_VERSION}_linux_${TARGETARCH}.zip$" tofu.sums | sed 's/  .*/  tofu.zip/' | sha256sum -c -); \
    unzip -j /tmp/tofu.zip tofu -d /usr/local/bin; \
    rm -f /tmp/tofu.zip /tmp/tofu.sums; \
    apt-get purge -y curl unzip; \
    apt-get autoremove -y; \
    rm -rf /var/lib/apt/lists/*; \
    ansible-playbook --version; \
    tofu version; \
    useradd --uid 65532 --create-home --home-dir /home/myinfra myinfra
COPY --from=build /out/myinfra /usr/local/bin/myinfra
USER 65532:65532
ENV HOME=/home/myinfra MYINFRA_HOME=/home/myinfra/.myinfra
WORKDIR /home/myinfra
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/myinfra"]
