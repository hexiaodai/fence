FROM --platform=$BUILDPLATFORM golang:1.20.3 as build
ENV GOOS=linux \
    CGO_ENABLED=0 \
    GO111MODULE=on
WORKDIR /fence
COPY . .
RUN go build -o fence ./cmd/ctrl

FROM --platform=$BUILDPLATFORM alpine:3.16
COPY --from=build /fence/fence /bin/fence
RUN chmod go+x /bin/fence
CMD ["fence", "ctrl"]
