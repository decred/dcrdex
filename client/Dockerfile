#
# Build the docker image
# $ docker build -t user/dcrdex -f client/Dockerfile .
#
# Create docker volume to store client data
# $ docker volume create --name=dcrdex_data
#
# Run the docker image, mapping web access port.
# $ docker run -d --rm -p 127.0.0.1:5758:5758 -v dcrdex_data:/dex/.dexc user/dcrdex
#

# frontend build
FROM node:19-buster-slim AS nodebuilder
WORKDIR /root/dex
COPY . .
# The release branches have the Webpack bundle checked so in is not necessary to
# build it again. These lines are only necessary on a development branch.
# RUN apt-get update && apt-get install -y git
# WORKDIR /root/dex/client/webserver/site/
# RUN npm clean-install
# RUN npm run build

# dexc binary build
FROM golang:1.19-alpine AS gobuilder
COPY --from=nodebuilder /root/dex/ /root/dex/
WORKDIR /root/dex/client/cmd/dexc/
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build
WORKDIR /root/dex/client/cmd/dexcctl/
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build

# Final image
FROM debian:buster-slim
RUN apt-get update && apt-get install -y ca-certificates
WORKDIR /dex
ENV HOME /dex
RUN mkdir -p /dex/.dexc && chown 1000 /dex/.dexc
USER 1000
COPY --from=gobuilder /root/dex/client/cmd/dexc/dexc ./
COPY --from=gobuilder /root/dex/client/cmd/dexcctl/dexcctl ./
EXPOSE 5758
CMD [ "./dexc", "--webaddr=0.0.0.0:5758" ]
