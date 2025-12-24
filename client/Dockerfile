#
# Build the docker image
# $ docker build -t user/bisonw -f client/Dockerfile --build-arg VERSION=v1.x.x .
#
# Create docker volume to store client data
# $ docker volume create --name=bisonw_data
#
# Run the docker image, mapping web access port.
# $ docker run -d --rm -p 127.0.0.1:5758:5758 -v bisonw_data:/dex/.dexc user/bisonw
#

# frontend build

# The image below is node:current-alpine3.22 (linux/amd64)
# It's pulled by the digest (immutable id) to avoid supply-chain attacks.
# Maintainer Note:
#    To update to a new digest, you must first manually pull the new image:
#    `docker pull node:<new version>`
#    Docker will print the digest of the new image after the pull has finished.
FROM node@sha256:809972647175c30a4c7763d3e6cc064dec588972af57e540e5a6f27442bb0845 AS nodebuilder
ARG VERSION="v1.0.5"
RUN apk add git && \
    git clone --depth 1 --branch $VERSION https://github.com/decred/dcrdex.git && \
    cd /dcrdex/client/webserver/site/ && \
    npm clean-install && \
    npm run build

# bisonw binary build

# The image below is golang:1.25.3-alpine3.22 (linux/amd64)
# It's pulled by the digest (immutable id) to avoid supply-chain attacks.
# Maintainer Note:
#    To update to a new digest, you must first manually pull the new image:
#    `docker pull golang:<new version>`
#    Docker will print the digest of the new image after the pull has finished.
FROM golang@sha256:aee43c3ccbf24fdffb7295693b6e33b21e01baec1b2a55acc351fde345e9ec34 AS gobuilder
COPY --from=nodebuilder /dcrdex/ /dcrdex/
RUN cd /dcrdex/client/cmd/bisonw/ && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" && \
    cd /dcrdex/client/cmd/bwctl/ && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w"

# Final image
FROM debian:bookworm-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates
WORKDIR /dex
ENV HOME=/dex
RUN mkdir -p /dex/.dexc && chown 1000 /dex/.dexc
USER 1000
COPY --from=gobuilder /dcrdex/client/cmd/bisonw/bisonw ./
COPY --from=gobuilder /dcrdex/client/cmd/bwctl/bwctl ./
# Using the local entrypoint.sh
COPY ./client/entrypoint.sh ./
EXPOSE 5758
ENTRYPOINT [ "./entrypoint.sh" ]
