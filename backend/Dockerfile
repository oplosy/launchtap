FROM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/indexer ./cmd/indexer \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 go build -trimpath -o /out/telegram ./cmd/telegram \
 && CGO_ENABLED=0 go build -trimpath -o /out/funding ./cmd/funding \
 && CGO_ENABLED=0 go build -trimpath -o /out/moderate ./cmd/moderate \
 && CGO_ENABLED=0 go build -trimpath -o /out/backfill-swap-sender ./cmd/backfill-swap-sender

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ /app/
USER 10001:10001
WORKDIR /app
CMD ["/app/api"]
