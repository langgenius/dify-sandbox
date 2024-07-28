# Makefile to install dependencies and build the project for amd64 and arm64 architectures

.PHONY: all install clean build_amd64 build_arm64

all: install detect-arch

install: detect-distro

detect-distro:
	@if [ -f /etc/debian_version ]; then \
		$(MAKE) install_ubuntu; \
	elif [ -f /etc/fedora-release ]; then \
		$(MAKE) install_fedora; \
	elif [ -f /etc/arch-release ]; then \
		$(MAKE) install_arch; \
	elif [ -f /etc/alpine-release ]; then \
		$(MAKE) install_alpine; \
	elif [ -f /etc/centos-release ]; then \
		$(MAKE) install_centos; \
	else \
		$(MAKE) unsupported; \
	fi

install_ubuntu:
	sudo apt-get install -y pkg-config gcc libseccomp-dev

install_fedora:
	sudo dnf install -y pkgconfig gcc libseccomp-devel

install_arch:
	sudo pacman -S --noconfirm pkg-config gcc libseccomp

install_alpine:
	sudo apk add pkgconfig gcc libseccomp-dev

install_centos:
	sudo yum install -y pkgconfig gcc libseccomp-devel

unsupported:
	@echo "Unsupported distribution"
	@exit 1

detect-arch:
	@if [ "$$(uname -m)" = "x86_64" ]; then \
		$(MAKE) build_amd64; \
	elif [ "$$(uname -m)" = "aarch64" ]; then \
		$(MAKE) build_arm64; \
	else \
		@echo "Unsupported architecture"; \
		@exit 1; \
	fi

clean:
	rm -f internal/core/runner/python/python.so
	rm -f internal/core/runner/nodejs/nodejs.so
	rm -f /tmp/sandbox-python/python.so
	rm -f /tmp/sandbox-nodejs/nodejs.so

build_amd64: clean
	@echo "Building Python lib for amd64"
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o internal/core/runner/python/python.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/python/main.go
	@echo "Building Nodejs lib for amd64"
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o internal/core/runner/nodejs/nodejs.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/nodejs/main.go
	@echo "Building main for amd64"
	GOOS=linux GOARCH=amd64 go build -o main -ldflags="-s -w" cmd/server/main.go

build_arm64: clean
	@echo "Building Python lib for arm64"
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o internal/core/runner/python/python.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/python/main.go
	@echo "Building Nodejs lib for arm64"
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o internal/core/runner/nodejs/nodejs.so -buildmode=c-shared -ldflags="-s -w" cmd/lib/nodejs/main.go
	@echo "Building main for arm64"
	GOOS=linux GOARCH=arm64 go build -o main -ldflags="-s -w" cmd/server/main.go