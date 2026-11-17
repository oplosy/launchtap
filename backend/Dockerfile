FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
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

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ /app/
USER 10001:10001
WORKDIR /app
CMD ["/app/api"]
