# aicon-mini

Hybrid BM25 + dense RAG prototype in Go (v1.23)

## Quick build & demo

```bash
go build -o ../../main
go run . demo
```

## Prime Lab integration

```bash
prime env init aicon-mini
prime env install
prime eval run aicon-mini '{"query":"GDPR stance ZYX"}'
```