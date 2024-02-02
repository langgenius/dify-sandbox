rm -rf /tmp/sandbox-python
rm -rf internal/core/runner/python/python.so
go build -o internal/core/runner/python/python.so -buildmode=c-shared cmd/lib/python/main.go