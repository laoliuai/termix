FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC

# Optional apt mirror override for faster builds in regions where the default
# archive.ubuntu.com / security.ubuntu.com are slow. Default keeps the upstream
# mirrors so the image is portable; override at build time with e.g.
#   --build-arg APT_MIRROR=mirrors.tuna.tsinghua.edu.cn
ARG APT_MIRROR=archive.ubuntu.com
RUN if [ "$APT_MIRROR" != "archive.ubuntu.com" ]; then \
      sed -i "s|archive.ubuntu.com|${APT_MIRROR}|g; s|security.ubuntu.com|${APT_MIRROR}|g" \
        /etc/apt/sources.list; \
    fi

# Base apt packages
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget gnupg lsb-release \
      git \
      build-essential pkg-config \
      tmux openssh-client \
      ripgrep jq vim less unzip xz-utils \
      postgresql-client \
      sudo \
      protobuf-compiler \
      software-properties-common \
  && rm -rf /var/lib/apt/lists/*

# Common curl flags for downloading large release artifacts. We add retry +
# stall detection because some egress paths to github releases can hang.
ENV DL_CURL_OPTS="-fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 30 --speed-limit 10240 --speed-time 30"

# gosu for the entrypoint privilege drop
ARG GOSU_VERSION=1.17
RUN ARCH="$(dpkg --print-architecture)" \
  && curl $DL_CURL_OPTS -o /usr/local/bin/gosu \
       "https://github.com/tianon/gosu/releases/download/${GOSU_VERSION}/gosu-${ARCH}" \
  && chmod +x /usr/local/bin/gosu \
  && gosu --version

# GitHub CLI installed from a release .deb. The official cli.github.com apt repo
# is unreliably slow from some regions; the github.com releases CDN tends to be
# more consistent. Pin a specific gh version to keep builds reproducible.
ARG GH_VERSION=2.91.0
RUN ARCH="$(dpkg --print-architecture)" \
  && curl $DL_CURL_OPTS -o /tmp/gh.deb \
       "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${ARCH}.deb" \
  && dpkg -i /tmp/gh.deb \
  && rm /tmp/gh.deb \
  && gh --version

# Go 1.25.x
ARG GO_VERSION=1.25.0
RUN ARCH_GO="$(dpkg --print-architecture)" \
  && case "$ARCH_GO" in \
       amd64) GO_ARCH=amd64 ;; \
       arm64) GO_ARCH=arm64 ;; \
       *) echo "unsupported architecture: $ARCH_GO" >&2 && exit 1 ;; \
     esac \
  && curl $DL_CURL_OPTS "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tgz \
  && tar -C /usr/local -xzf /tmp/go.tgz \
  && rm /tmp/go.tgz
ENV PATH=/usr/local/go/bin:$PATH

# Node.js 22 LTS via NodeSource
RUN curl $DL_CURL_OPTS https://deb.nodesource.com/setup_22.x | bash - \
  && apt-get install -y --no-install-recommends nodejs \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
