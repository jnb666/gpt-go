#!/bin/bash
# script to launch llama-server instance with default opts for gpt-oss

BIN_DIR="$HOME/llama.cpp/build/bin"
MODEL_DIR="/usr/local/data/gguf"
DEFAULT_MODEL="gpt-oss-20b-unsloth-F16.gguf"

MODEL_FILE="${1:-$DEFAULT_MODEL}"
EXTRA_ARGS="${@:2}"

$BIN_DIR/llama-server -m "${MODEL_DIR}/${MODEL_FILE}" \
     --host 0.0.0.0 --port 8080 --ctx-size 32768 --keep 200 \
     --jinja -ub 2048 -b 2048 -ngl 99  -fa $EXTRA_ARGS

