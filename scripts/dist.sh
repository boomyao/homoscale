#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
BUILD_DIR="${BUILD_DIR:-${DIST_DIR}/build}"
STAGE_DIR="${STAGE_DIR:-${DIST_DIR}/stage}"
TARGETS="${TARGETS:-$(go env GOOS)/$(go env GOARCH)}"
VERSION="${VERSION:-}"
COMMIT="${COMMIT:-}"
BUILD_DATE="${BUILD_DATE:-}"

if [[ -z "${VERSION}" ]]; then
  if git -C "${ROOT_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    VERSION="$(git -C "${ROOT_DIR}" describe --tags --always --dirty)"
  else
    VERSION="dev"
  fi
fi

if [[ -z "${COMMIT}" ]]; then
  if git -C "${ROOT_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    COMMIT="$(git -C "${ROOT_DIR}" rev-parse --short HEAD)"
  else
    COMMIT="none"
  fi
fi

if [[ -z "${BUILD_DATE}" ]]; then
  BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
fi

rm -rf "${BUILD_DIR}" "${STAGE_DIR}"
mkdir -p "${DIST_DIR}" "${BUILD_DIR}" "${STAGE_DIR}"

package_target() {
  local goos="$1"
  local goarch="$2"
  local ext=""
  local binary_name="homoscale"
  local archive_name="homoscale_${VERSION}_${goos}_${goarch}"
  local out_bin="${BUILD_DIR}/${archive_name}/${binary_name}"
  local stage_path="${STAGE_DIR}/${archive_name}"

  if [[ "${goos}" == "windows" ]]; then
    ext=".exe"
    binary_name="homoscale.exe"
    out_bin="${BUILD_DIR}/${archive_name}/${binary_name}"
  fi

  mkdir -p "$(dirname "${out_bin}")" "${stage_path}"

  echo "==> building ${goos}/${goarch}"
  (
    cd "${ROOT_DIR}"
    GOOS="${goos}" GOARCH="${goarch}" \
      go build \
      -trimpath \
      -ldflags="-s -w -X homoscale/internal/homoscale.version=${VERSION} -X homoscale/internal/homoscale.commit=${COMMIT} -X homoscale/internal/homoscale.date=${BUILD_DATE}" \
      -o "${out_bin}" \
      ./cmd/homoscale
  )

  cp "${out_bin}" "${stage_path}/${binary_name}"
  cp "${ROOT_DIR}/README.md" "${stage_path}/README.md"
  mkdir -p "${stage_path}/examples"
  cp "${ROOT_DIR}/examples/"*.yaml "${stage_path}/examples/"

  if [[ "${goos}" == "windows" ]]; then
    (
      cd "${STAGE_DIR}"
      zip -rq "${DIST_DIR}/${archive_name}.zip" "${archive_name}"
    )
  else
    tar -C "${STAGE_DIR}" -czf "${DIST_DIR}/${archive_name}.tar.gz" "${archive_name}"
  fi
}

for target in ${TARGETS}; do
  goos="${target%/*}"
  goarch="${target#*/}"
  if [[ -z "${goos}" || -z "${goarch}" || "${goos}" == "${goarch}" ]]; then
    echo "invalid target: ${target}" >&2
    exit 1
  fi
  package_target "${goos}" "${goarch}"
done

checksum_file="${DIST_DIR}/SHA256SUMS"
rm -f "${checksum_file}"
(
  cd "${DIST_DIR}"
  : > "SHA256SUMS"
  for artifact in homoscale_"${VERSION}"_*; do
    [[ -f "${artifact}" ]] || continue
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "${artifact}" >> "SHA256SUMS"
    else
      shasum -a 256 "${artifact}" >> "SHA256SUMS"
    fi
  done
)

echo "==> artifacts written to ${DIST_DIR}"
