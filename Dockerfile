FROM golang:1.22-bookworm AS go-build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/hlmdsrv ./cmd/hlmdsrv

FROM python:3.12-slim-bookworm

WORKDIR /app

# "minimal" ships MDTraj only, which covers the analyze surface at a fraction of
# the image size. "full" adds MDAnalysis for the fixture and compatibility paths.
ARG MDSRV_PYTHON_BACKENDS=minimal

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN if [ "$MDSRV_PYTHON_BACKENDS" = "full" ]; then \
      pip install --no-cache-dir mdtraj MDAnalysis; \
    else \
      pip install --no-cache-dir mdtraj; \
    fi

COPY scripts ./scripts
COPY schema ./schema
COPY examples ./examples
COPY docs ./docs
COPY --from=go-build /out/hlmdsrv /usr/local/bin/hlmdsrv

ENV MDSRV_PYTHON="python3"

ENTRYPOINT ["hlmdsrv"]
CMD ["doctor"]
