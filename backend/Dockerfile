FROM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/indexer ./cmd/indexer \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate  && CGO_ENABLED=0 go build -trimpath -o /out/telegram ./cmd/telegram

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ /app/
USER 10001:10001
WORKDIR /app
CMD ["/app/api"]
