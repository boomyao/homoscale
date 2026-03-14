#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_APP_DIR="${ROOT_DIR}/android/app"
OUTPUT_DIR="${ANDROID_APP_DIR}/build/generated/jniLibs/arm64-v8a"

find_ndk_root() {
  if [[ -n "${ANDROID_NDK_ROOT:-}" && -d "${ANDROID_NDK_ROOT}" ]]; then
    printf '%s\n' "${ANDROID_NDK_ROOT}"
    return 0
  fi

  local sdk_root="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
  if [[ -n "${sdk_root}" && -d "${sdk_root}/ndk" ]]; then
    ls -d "${sdk_root}"/ndk/* 2>/dev/null | sort -V | tail -1
    return 0
  fi

  if [[ -d "${HOME}/Library/Android/sdk/ndk" ]]; then
    ls -d "${HOME}/Library/Android/sdk/ndk"/* 2>/dev/null | sort -V | tail -1
    return 0
  fi

  return 1
}

find_toolchain_root() {
  local ndk_root="$1"
  local base="${ndk_root}/toolchains/llvm/prebuilt"
  if [[ ! -d "${base}" ]]; then
    return 1
  fi
  ls -d "${base}"/* 2>/dev/null | head -1
}

NDK_ROOT="$(find_ndk_root || true)"
if [[ -z "${NDK_ROOT}" || ! -d "${NDK_ROOT}" ]]; then
  echo "Android NDK not found. Set ANDROID_SDK_ROOT/ANDROID_HOME or ANDROID_NDK_ROOT first." >&2
  exit 1
fi

TOOLCHAIN_ROOT="$(find_toolchain_root "${NDK_ROOT}" || true)"
if [[ -z "${TOOLCHAIN_ROOT}" || ! -d "${TOOLCHAIN_ROOT}" ]]; then
  echo "Android NDK LLVM toolchain not found under ${NDK_ROOT}" >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"

CC_BIN="${TOOLCHAIN_ROOT}/bin/aarch64-linux-android28-clang"
if [[ ! -x "${CC_BIN}" ]]; then
  echo "Android compiler not found: ${CC_BIN}" >&2
  exit 1
fi

echo "==> building Go Android bridge"
CGO_ENABLED=1 \
GOOS=android \
GOARCH=arm64 \
CC="${CC_BIN}" \
go build \
  -tags=cmfa,with_gvisor \
  -buildmode=c-shared \
  -trimpath \
  -o "${OUTPUT_DIR}/libhomoscale.so" \
  ./mobilelib
