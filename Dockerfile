FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /out/or3-intern ./cmd/or3-intern

FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates sqlite3 \
	&& rm -rf /var/lib/apt/lists/*

ENV OR3_CONFIG=/config/config.json
WORKDIR /workspace
COPY --from=build /out/or3-intern /usr/local/bin/or3-intern

EXPOSE 9100
CMD ["or3-intern", "--config", "/config/config.json", "service"]
