FROM ubuntu:24.04@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55
RUN apt-get update && apt-get install -y --no-install-recommends apparmor systemd shellcheck python3 python3-yaml && rm -rf /var/lib/apt/lists/*
WORKDIR /workspace
COPY scripts /workspace/scripts
COPY tests/conformance/fixture /workspace/fixture
COPY .github/workflows/builder-conformance.yml /workspace/workflow.yml
# Compiler/parser checks only: no profile loading, services, namespaces or caps.
CMD ["bash", "-c", "set -e; apparmor_parser --skip-kernel-load --skip-read-cache fixture/apparmor.profile; shellcheck scripts/builder-conformance*.sh fixture/start-candidate.sh; python3 -c 'import pathlib,ast,yaml; [ast.parse(p.read_text()) for p in pathlib.Path(\"fixture\").glob(\"*.py\")]; yaml.safe_load(pathlib.Path(\"workflow.yml\").read_text())'"]
