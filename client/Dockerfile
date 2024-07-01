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

# The image below is node:current-alpine3.18 (linux/amd64)
# It's pulled by the digest (immutable id) to avoid supply-chain attacks.
# Maintainer Note:
#    To update to a new digest, you must first manually pull the new image:
#    `docker pull node:<new version>`
#    Docker will print the digest of the new image after the pull has finished.
FROM node@sha256:d75175d449921d06250afd87d51f39a74fc174789fa3c50eba0d3b18369cc749 AS nodebuilder
WORKDIR /root/dex
COPY . .
RUN apk add git
WORKDIR /root/dex/client/webserver/site/
RUN npm clean-install
RUN npm run build

# bisonw binary build

# The image below is golang:1.21.0-alpine3.18 (linux/amd64)
# It's pulled by the digest (immutable id) to avoid supply-chain attacks.
# Maintainer Note:
#    To update to a new digest, you must first manually pull the new image:
#    `docker pull golang:<new version>`
#    Docker will print the digest of the new image after the pull has finished.
FROM golang@sha256:445f34008a77b0b98bf1821bf7ef5e37bb63cc42d22ee7c21cc17041070d134f AS gobuilder
COPY --from=nodebuilder /root/dex/ /root/dex/
WORKDIR /root/dex/client/cmd/bisonw/
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build
WORKDIR /root/dex/client/cmd/bwctl/
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build

# Final image
FROM debian:buster-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates
WORKDIR /dex
ENV HOME /dex
RUN mkdir -p /dex/.dexc && chown 1000 /dex/.dexc
USER 1000
COPY --from=gobuilder /root/dex/client/cmd/bisonw/bisonw ./
COPY --from=gobuilder /root/dex/client/cmd/bwctl/bwctl ./
EXPOSE 5758
CMD [ "./bisonw", "--webaddr=0.0.0.0:5758" ]
