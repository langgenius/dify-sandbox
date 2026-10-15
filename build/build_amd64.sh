rm -f internal/core/runner/python/python.so
rm -f internal/core/runner/nodejs/nodejs.so
rm -f /tmp/sandbox-python/python.so
rm -f /tmp/sandbox-nodejs/nodejs.so
echo "Building Python lib"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o internal/core/runner/python/python.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/python/main.go &&
echo "Building Nodejs lib" &&
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o internal/core/runner/nodejs/nodejs.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/nodejs/main.go &&
echo "Building main" &&
GOOS=linux GOARCH=amd64 go build -o main -ldflags="-s -w" cmd/server/main.go
echo "Building env"
GOOS=linux GOARCH=amd64 go build -o env -ldflags="-s -w" cmd/dependencies/init.go
# Explicit Builder build artifacts; ordinary builds retain their existing owner.
if [ "${SANDBOX_PROFILE:-}" = builder ]; then
  GOOS=linux GOARCH=amd64 go build -o request-worker ./cmd/request-worker || exit 1
  GOOS=linux GOARCH=amd64 go build -o environment-manifest ./cmd/environment-manifest || exit 1
fi
