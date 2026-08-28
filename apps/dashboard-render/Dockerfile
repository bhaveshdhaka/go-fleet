FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=readonly go build -trimpath -o /out/dashboard-render .
FROM alpine:3.20
COPY --from=build /out/dashboard-render /dashboard-render
ENV HTML_DIR=/srv/html \
    STATE_DIR=/state \
    OUT=/webroot/index.html \
    PORT=9090
VOLUME /data
EXPOSE 9090
ENTRYPOINT ["/dashboard-render"]
