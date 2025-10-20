#
# Build the docker image
# $ docker build -t user/bisonw -f client/Dockerfile .
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
WORKDIR /root/dex
COPY . .
RUN apk add git
WORKDIR /root/dex/client/webserver/site/
RUN npm clean-install
RUN npm run build

# bisonw binary build

# The image below is golang:1.25.3-alpine3.22 (linux/amd64)
# It's pulled by the digest (immutable id) to avoid supply-chain attacks.
# Maintainer Note:
#    To update to a new digest, you must first manually pull the new image:
#    `docker pull golang:<new version>`
#    Docker will print the digest of the new image after the pull has finished.
FROM golang@sha256:aee43c3ccbf24fdffb7295693b6e33b21e01baec1b2a55acc351fde345e9ec34 AS gobuilder
COPY --from=nodebuilder /root/dex/ /root/dex/
WORKDIR /root/dex/client/cmd/bisonw/
RUN CGO_ENABLED=0 GOOS=linux go build
WORKDIR /root/dex/client/cmd/bwctl/
RUN CGO_ENABLED=0 GOOS=linux go build

# Final image
FROM debian:bookworm-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates
WORKDIR /dex
ENV HOME=/dex
RUN mkdir -p /dex/.dexc && chown 1000 /dex/.dexc
USER 1000
COPY --from=gobuilder /root/dex/client/cmd/bisonw/bisonw ./
COPY --from=gobuilder /root/dex/client/cmd/bwctl/bwctl ./
COPY ./client/entrypoint.sh ./
EXPOSE 5758
ENTRYPOINT [ "./entrypoint.sh" ]
