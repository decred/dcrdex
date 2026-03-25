# Dockerfile for cross-compiling bisonw-desktop.exe for Windows from Linux.
#
# Prerequisites:
#   The site bundle must be built before running the docker build:
#     cd client/webserver/site && npm clean-install && npm run build
#
# Build (from the repo root):
#   docker build -t bisonw-desktop-windows -f client/cmd/bisonw-desktop/windows/Dockerfile .
#
# Build with XMR support:
#   docker build -t bisonw-desktop-windows --build-arg XMR=1 -f client/cmd/bisonw-desktop/windows/Dockerfile .
#
# Extract the exe and DLLs:
#   mkdir -p build && docker run --rm bisonw-desktop-windows tar -cf - -C /out . | tar -xf - -C ./build

FROM golang:1.24-bookworm

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    gcc-mingw-w64-x86-64 \
    g++-mingw-w64-x86-64 \
    mingw-w64-tools \
    unzip \
    && rm -rf /var/lib/apt/lists/*

ARG XMR=0
ARG WEBVIEW2_VERSION=1.0.3856.49

COPY . /src
WORKDIR /src/client/cmd/bisonw-desktop

# Fetch the WebView2 NuGet package for the runtime DLL (WebView2Loader.dll).
RUN mkdir -p /tmp/webview2 && \
    curl -sSL "https://www.nuget.org/api/v2/package/Microsoft.Web.WebView2/${WEBVIEW2_VERSION}" -o /tmp/webview2.zip && \
    unzip -q /tmp/webview2.zip -d /tmp/webview2 && \
    rm /tmp/webview2.zip

# Create a stub EventToken.h. The webview_go module's bundled WebView2.h
# includes this Windows SDK header, which MinGW does not provide.
RUN mkdir -p /tmp/winsdk && \
    printf '#pragma once\ntypedef struct EventRegistrationToken { __int64 value; } EventRegistrationToken;\n' \
    > /tmp/winsdk/EventToken.h

# Generate a MinGW import library for the XMR DLL. The MinGW linker cannot
# link directly against a .dll. Named .a (not .dll.a) because webview_go's
# CGO LDFLAGS include -static, which makes the linker skip .dll.a files.
RUN if [ "$XMR" = "1" ]; then \
        cd /src/client/asset/xmr/lib/windows-amd64 && \
        gendef libwallet2_api_c.dll && \
        x86_64-w64-mingw32-dlltool -d libwallet2_api_c.def \
            -l libwallet2_api_c.a -D libwallet2_api_c.dll; \
    fi

RUN mkdir -p /out

ENV GOOS=windows
ENV GOARCH=amd64
ENV CGO_ENABLED=1
ENV CC=x86_64-w64-mingw32-gcc
ENV CXX=x86_64-w64-mingw32-g++
ENV CGO_CXXFLAGS="-I/tmp/winsdk"

RUN if [ "$XMR" = "1" ]; then \
        go build -v -tags xmr -ldflags="-H windowsgui" -o /out/bisonw-desktop.exe; \
    else \
        go build -v -ldflags="-H windowsgui" -o /out/bisonw-desktop.exe; \
    fi

# Bundle runtime DLLs that are not present on a stock Windows install.
RUN cp /tmp/webview2/runtimes/win-x64/native/WebView2Loader.dll /out/ && \
    cp /usr/x86_64-w64-mingw32/lib/libwinpthread-1.dll /out/

RUN if [ "$XMR" = "1" ]; then \
        cp /src/client/asset/xmr/lib/windows-amd64/libwallet2_api_c.dll /out/; \
    fi
