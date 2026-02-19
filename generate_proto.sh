#!/bin/bash
set -euo pipefail

buf generate
if ! command -v protoc-gen-pubsub >/dev/null 2>&1; then
  go install github.com/bufbuild/protoschema-plugins/cmd/protoc-gen-pubsub@v0.5.1
fi
rm -rf gen/proto/infra/pubsub
buf generate --template buf.pubsub.gen.yaml

# Clean up unwanted pubsub proto files - keep only the main schema files
echo "Cleaning up redundant pubsub proto files..."
PUBSUB_DIR="gen/proto/infra/pubsub"

KEEP_FILES=$(grep -E "^\s+- '" buf.pubsub.gen.yaml | sed "s/.*- '//g" | sed "s/'.*//g" | sed 's/$/.pubsub.proto/')

shopt -s nullglob
for file in "$PUBSUB_DIR"/*.pubsub.proto; do
  filename=$(basename "$file")
  if echo "$KEEP_FILES" | grep -qx "$filename"; then
    echo "  Kept: $filename"
  else
    rm "$file"
    echo "  Removed: $filename"
  fi
done
shopt -u nullglob

# Generate Config Connector PubSubSchema manifests from the pubsub schema protos.
# One schema manifest is generated for each `*.pubsub.proto` file.
(cd tools/pubsubschema-gen && go run . \
  --pubsub-dir "../../$PUBSUB_DIR" \
  --output-dir "../../namespaces/core-app/base/schemas")
