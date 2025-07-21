#!/bin/bash

# Docker build generation script
# Purpose: Generate final Dockerfiles from version configuration and templates
# Usage: ./generate.sh [production|test] [amd64|arm64]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS_FILE="${SCRIPT_DIR}/versions.yaml"
TEMPLATES_DIR="${SCRIPT_DIR}/templates"
OUTPUT_DIR="${SCRIPT_DIR}"

# 解析命令行参数
ENVIRONMENT="${1:-production}"
ARCHITECTURE="${2:-amd64}"

if [[ ! "$ENVIRONMENT" =~ ^(production|test)$ ]]; then
    echo "Error: Environment type must be 'production' or 'test'"
    echo "Usage: $0 [production|test] [amd64|arm64]"
    exit 1
fi

if [[ ! "$ARCHITECTURE" =~ ^(amd64|arm64)$ ]]; then
    echo "Error: Architecture must be 'amd64' or 'arm64'"
    echo "Usage: $0 [production|test] [amd64|arm64]"
    exit 1
fi

# Check dependencies
if ! command -v yq &> /dev/null; then
    echo "Error: yq tool is required to parse YAML files"
    echo "Installation: https://github.com/mikefarah/yq#install"
    exit 1
fi

if [[ ! -f "$VERSIONS_FILE" ]]; then
    echo "Error: Version configuration file not found: $VERSIONS_FILE"
    exit 1
fi

# Read version configuration
echo "Reading version configuration..."
PYTHON_VERSION=$(yq eval '.versions.python' "$VERSIONS_FILE")
GOLANG_VERSION=$(yq eval '.versions.golang' "$VERSIONS_FILE")
NODEJS_VERSION=$(yq eval '.versions.nodejs' "$VERSIONS_FILE")
PYTHON_PACKAGES=$(yq eval '.versions.python_packages' "$VERSIONS_FILE")
DEBIAN_MIRROR=$(yq eval '.mirrors.debian' "$VERSIONS_FILE")
NODEJS_MIRROR=$(yq eval '.mirrors.nodejs' "$VERSIONS_FILE")
GOLANG_MIRROR=$(yq eval '.mirrors.golang' "$VERSIONS_FILE")

# Select template file
TEMPLATE_FILE="${TEMPLATES_DIR}/${ENVIRONMENT}.dockerfile"
if [[ ! -f "$TEMPLATE_FILE" ]]; then
    echo "Error: Template file not found: $TEMPLATE_FILE"
    exit 1
fi

# Generate output filename (unified naming scheme)
OUTPUT_FILE="${OUTPUT_DIR}/${ARCHITECTURE}-${ENVIRONMENT}.gen.dockerfile"

echo "Generating Dockerfile..."
echo "  Environment: $ENVIRONMENT"
echo "  Architecture: $ARCHITECTURE"
echo "  Template: $TEMPLATE_FILE"
echo "  Output: $OUTPUT_FILE"

# Architecture mapping (for Node.js filename)
case "$ARCHITECTURE" in
    "amd64")
        NODEJS_ARCH="x64"
        ;;
    "arm64")
        NODEJS_ARCH="arm64"
        ;;
esac

# Generate Dockerfile
sed -e "s/\${PYTHON_VERSION}/${PYTHON_VERSION}/g" \
    -e "s/\${GOLANG_VERSION}/${GOLANG_VERSION}/g" \
    -e "s/\${NODEJS_VERSION}/${NODEJS_VERSION}/g" \
    -e "s|\${PYTHON_PACKAGES}|${PYTHON_PACKAGES}|g" \
    -e "s|\${DEBIAN_MIRROR}|${DEBIAN_MIRROR}|g" \
    -e "s|\${NODEJS_MIRROR}|${NODEJS_MIRROR}|g" \
    -e "s|\${GOLANG_MIRROR}|${GOLANG_MIRROR}|g" \
    -e "s/\${TARGETARCH}/${ARCHITECTURE}/g" \
    -e "s/__ARCH__/${NODEJS_ARCH}/g" \
    "$TEMPLATE_FILE" > "$OUTPUT_FILE"

echo "Dockerfile generated: $OUTPUT_FILE"

# Convenience function to generate all combinations
generate_all() {
    echo "Generating all Dockerfile combinations..."
    
    for env in production test; do
        for arch in amd64 arm64; do
            echo "Generating $env-$arch..."
            "$0" "$env" "$arch"
        done
    done
    
    echo "All Dockerfiles generated!"
}

# If no arguments, generate all combinations
if [[ $# -eq 0 ]]; then
    generate_all
fi 