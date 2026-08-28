FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=readonly go build -trimpath -o /out/canary .

FROM busybox:1.36
COPY --from=build /out/canary /canary
ENV PORT=8080
USER 65532:65532
EXPOSE 8080
CMD ["/canary"]
