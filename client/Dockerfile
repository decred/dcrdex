# Some instructions on how to work with Docker: https://github.com/decred/dcrdex#docker.

# frontend UI build
FROM node:16 AS nodebuilder
WORKDIR /root/dex
COPY . .
WORKDIR /root/dex/client/webserver/site/
RUN npm clean-install
RUN npm run build

# dexc binary build
FROM golang:1.18 AS gobuilder
COPY --from=nodebuilder /root/dex/ /root/dex/
WORKDIR /root/dex/client/cmd/dexc/
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build
WORKDIR /root/dex/client/cmd/dexcctl/
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build

# Final image
FROM alpine:3.14
WORKDIR /root
COPY --from=gobuilder /root/dex/client/cmd/dexc/dexc ./
COPY --from=gobuilder /root/dex/client/cmd/dexcctl/dexcctl ./
COPY --from=gobuilder /root/dex/client/webserver/site ./site
EXPOSE 5758
ENTRYPOINT [ "./dexc" ]
