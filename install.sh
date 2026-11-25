#!/bin/sh
# Installs the newest release of wrktree.
#
#   curl -sSL https://raw.githubusercontent.com/dev-shetty/wrktree/main/install.sh | sh
#
# Set WRKTREE_INSTALL_DIR to select the folder. The default is /usr/local/bin
# if you can write to it, and ~/.local/bin if you cannot.
set -eu

repo="dev-shetty/wrktree"

case "$(uname -s)" in
  Darwin) os="Darwin" ;;
  Linux) os="Linux" ;;
  *) echo "wrktree: unsupported system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="x86_64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) echo "wrktree: unsupported processor: $(uname -m)" >&2; exit 1 ;;
esac

file="wrktree_${os}_${arch}.tar.gz"
url="https://github.com/${repo}/releases/latest/download"

dir="${WRKTREE_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then
    dir="/usr/local/bin"
  else
    dir="$HOME/.local/bin"
  fi
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${file}"
curl -fsSL "${url}/${file}" -o "${tmp}/${file}"
curl -fsSL "${url}/checksums.txt" -o "${tmp}/checksums.txt"

want="$(awk -v f="$file" '$2 == f { print $1 }' "${tmp}/checksums.txt")"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "${tmp}/${file}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  got="$(shasum -a 256 "${tmp}/${file}" | awk '{ print $1 }')"
else
  echo "wrktree: no sha256sum or shasum command: cannot check the download" >&2
  exit 1
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
  echo "wrktree: the checksum of ${file} is not correct" >&2
  exit 1
fi

tar -xzf "${tmp}/${file}" -C "$tmp" wrktree
mkdir -p "$dir"
install -m 755 "${tmp}/wrktree" "${dir}/wrktree"

echo "Installed ${dir}/wrktree"
case ":${PATH}:" in
  *":${dir}:"*) ;;
  *) echo "Add ${dir} to your PATH." ;;
esac
echo 'Then add this line to ~/.zshrc or ~/.bashrc:  eval "$(wrktree init zsh)"'
