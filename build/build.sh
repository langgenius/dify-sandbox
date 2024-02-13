rm -rf /tmp/sandbox-python
rm -rf internal/core/runner/python/python.so
go build -o internal/core/runner/python/python.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/python/main.go
go build -o main -ldflags="-s -w" cmd/server/main.go