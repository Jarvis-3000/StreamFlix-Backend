# Build the binary against the Go version declared in go.mod. This tag must be
# >= the go directive there, or `go mod download` fails outright: the toolchain
# in the image cannot build a module that asks for a newer one.
FROM golang:1.25-bookworm AS build

WORKDIR /src

# Copy the module files first so dependency download is cached independently of
# the source, which changes far more often.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is not needed by any dependency, so build a static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/streamflix .

# Runtime image. The pipeline shells out to ffmpeg and ffprobe, so the runtime
# needs them on PATH — this is why the project cannot use the default Go
# buildpack on Railway or Render, neither of which ships ffmpeg.
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=build /out/streamflix /app/streamflix

# Working directories the app creates and writes to at runtime. Declaring them
# here means the first upload does not race directory creation.
RUN mkdir -p /app/uploads /app/processed /app/data

# Documentation only; the platform injects PORT and the app binds to it.
EXPOSE 8080

CMD ["/app/streamflix"]
