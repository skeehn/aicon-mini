FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /out/aicon .

FROM alpine:3.20
RUN adduser -D aicon
COPY --from=build /out/aicon /usr/local/bin/aicon
USER aicon
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["/usr/local/bin/aicon"]
CMD ["serve"]
