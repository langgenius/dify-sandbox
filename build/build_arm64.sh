rm -f internal/core/runner/python/python.so
rm -f internal/core/runner/nodejs/nodejs.so
rm -f internal/core/runner/uv/uv.so
rm -f /tmp/sandbox-python/python.so
rm -f /tmp/sandbox-nodejs/nodejs.so
rm -f /tmp/sandbox-uv/uv.so
echo "Building Python lib"
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o internal/core/runner/python/python.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/python/main.go &&
echo "Building Nodejs lib" &&
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o internal/core/runner/nodejs/nodejs.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/nodejs/main.go &&
echo "Building uv lib" &&
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o internal/core/runner/uv/uv.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/uv/main.go &&
echo "Building main" &&
GOOS=linux GOARCH=arm64 go build -o main -ldflags="-s -w" cmd/server/main.go
echo "Building env"
GOOS=linux GOARCH=arm64 go build -o env -ldflags="-s -w" cmd/dependencies/init.go
