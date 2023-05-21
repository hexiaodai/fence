FROM --platform=$BUILDPLATFORM golang:1.20.3 as build
ENV GOOS=linux \
    CGO_ENABLED=0 \
    GO111MODULE=on
WORKDIR /fence
COPY . .
RUN go build -o fence-proxy ./cmd/proxy

FROM --platform=$BUILDPLATFORM alpine:3.16
COPY --from=build /fence/fence-proxy /bin/fence-proxy
RUN chmod go+x /bin/fence-proxy
CMD ["fence-proxy", "proxy"]
