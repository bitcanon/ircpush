#!/bin/bash
set -euo pipefail

PROJDIR="$(cd "$(dirname "$0")"/.. && pwd)"

# Read version from ./version file (expected format: v1.0.4 or 1.0.4)
RAW_VERSION="$(tr -d ' \n' < "${PROJDIR}/version")"
if [[ -z "${RAW_VERSION}" ]]; then
  echo "version file empty"
  exit 1
fi

# Normalize: ensure TAG has leading v, VERSION without leading v
if [[ "${RAW_VERSION}" =~ ^v ]]; then
  TAG="${RAW_VERSION}"
  VERSION="${RAW_VERSION#v}"
else
  VERSION="${RAW_VERSION}"
  TAG="v${RAW_VERSION}"
fi

USER="bitcanon"
REPO="ircpush"
BINARY="${REPO}"

# Inject version into the binary via -ldflags
LD_PKG="github.com/${USER}/${REPO}/cmd"
GO_LDFLAGS="-s -w -X ${LD_PKG}.buildVersion=${TAG}"

echo "Release version: ${VERSION} (tag: ${TAG})"

cd "${PROJDIR}"

echo "Running tests..."
go test ./...
echo "Tests passed."

FILELIST=""

for ARCH in amd64 386 arm64; do
  for OS in darwin linux windows freebsd; do
    if [[ "${OS}" == "darwin" && "${ARCH}" == "386" ]]; then
      continue
    fi

    BINFILE="${BINARY}"
    [[ "${OS}" == "windows" ]] && BINFILE="${BINFILE}.exe"

    rm -f "${BINFILE}"

    echo "Building ${OS}/${ARCH}..."
    GOOS="${OS}" GOARCH="${ARCH}" CGO_ENABLED=0 \
      go build -trimpath -ldflags "${GO_LDFLAGS}" -o "${BINFILE}" .

    if [[ "${OS}" == "windows" ]]; then
      ARCHIVE="${BINARY}-${OS}-${ARCH}-${VERSION}.zip"
      zip -q "${ARCHIVE}" "${BINFILE}"
      rm -f "${BINFILE}"
    else
      ARCHIVE="${BINARY}-${OS}-${ARCH}-${VERSION}.tgz"
      tar -czf "${ARCHIVE}" "${BINFILE}"
    fi

    FILELIST="${FILELIST} ${PROJDIR}/${ARCHIVE}"
  done
done

echo "Creating Git tag ${TAG}..."
git tag -a "${TAG}" -m "Release ${TAG}"
git push --follow-tags

echo "Publishing GitHub release..."
gh release create "${TAG}" ${FILELIST}

echo "Cleaning up archives..."
rm -f ${FILELIST}

echo "Done. Binaries embed version ${TAG}."
echo "To bump version: edit ./version (e.g. v1.0.6), commit, rerun this script."