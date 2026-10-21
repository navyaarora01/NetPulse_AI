FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api && \
 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/worker ./cmd/worker && \
 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/simulator ./cmd/simulator && \
 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate
FROM alpine:3.22.1
RUN apk add --no-cache ca-certificates && addgroup -S netpulse && adduser -S netpulse -G netpulse
COPY --from=build /out/ /app/
USER netpulse
EXPOSE 8080 8081
CMD ["/app/api"]
