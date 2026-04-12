FROM golang:1.25rc1-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build
COPY . .

# Rewrite local replace to a relative path inside the build context
COPY --from=gotreesitter . /gotreesitter
RUN sed -i 's|replace github.com/odvcencio/gotreesitter => .*|replace github.com/odvcencio/gotreesitter => /gotreesitter|' go.mod
RUN go mod tidy && CGO_ENABLED=0 go build -o /canopy ./cmd/canopy/

# Runtime
FROM alpine:3.21
RUN apk add --no-cache git && git config --system --add safe.directory '*'
COPY --from=builder /canopy /usr/local/bin/canopy
ENTRYPOINT ["canopy"]
