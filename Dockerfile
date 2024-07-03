FROM --platform=$BUILDPLATFORM golang:1.21-bookworm as builder

ARG TARGETOS TARGETARCH
RUN echo "$TARGETARCH" | sed 's,arm,aarch,;s,amd,x86_,' > /tmp/arch
RUN apt-get update && apt-get install -y "gcc-$(tr _ - < /tmp/arch)-linux-gnu" && apt-get clean && rm -rf /var/lib/apt/lists/*

ENV GOOS $TARGETOS
ENV GOARCH $TARGETARCH

# Move to working directory /build
WORKDIR /build

# Copy and download dependency using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN go env
RUN CGO_ENABLED=1 CC=$(cat /tmp/arch)-linux-gnu-gcc go build -o main
RUN if [ -e /go/bin/${TARGETOS}_${TARGETARCH} ]; then mv /go/bin/${TARGETOS}_${TARGETARCH}/* /go/bin/; fi

FROM debian:bookworm as final

# Copy the binaries and entrypoint from the builder image.
COPY --from=builder /build/main /bin/
COPY --from=builder /build/public /public/
COPY --from=builder /build/views /views/

RUN addgroup --system --gid 1000 go && adduser --system --uid 1000 --ingroup go go
RUN apt-get update && apt-get -y install ca-certificates && apt-get clean && rm -rf /var/lib/apt/lists

RUN chown -R go:go /bin
RUN chown -R go:go /public
RUN chown -R go:go /views

USER go

ENTRYPOINT [ "/bin/main" ]
