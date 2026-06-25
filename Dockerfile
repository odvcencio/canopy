FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build
COPY . .

# Build with the curated programming-language grammar subset via the
# ferrous-wheel build script (scripts/build.fw). This keeps the image off
# gotreesitter's full ~206-grammar registry (~22MB of mostly-unused blobs);
# the .fw script is the single source of truth for which grammars ship.
# gotreesitter is released, so no --build-context is needed — modules resolve
# straight from the proxy.
RUN go install m31labs.dev/ferrous-wheel/cmd/ferrous-wheel@v0.5.0
RUN go mod tidy && CGO_ENABLED=0 ferrous-wheel run scripts/build.fw && cp bin/canopy /canopy

# Runtime
FROM alpine:3.21
RUN apk add --no-cache git && git config --system --add safe.directory '*'
COPY --from=builder /canopy /usr/local/bin/canopy
ENTRYPOINT ["canopy"]
