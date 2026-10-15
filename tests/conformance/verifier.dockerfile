FROM python:3.12.12-slim-bookworm@sha256:593bd06efe90efa80dc4eee3948be7c0fde4134606dd40d8dd8dbcade98e669c
RUN pip install --no-cache-dir pytest==8.3.5 jsonschema==4.23.0
WORKDIR /workspace
COPY scripts /workspace/scripts
COPY tests/conformance /workspace/tests/conformance
USER 65534
CMD ["python", "-m", "pytest", "tests/conformance/test_release_manifest.py", "-q"]
