#!/bin/sh
#
# Adapted from the pressly/goose installer: Copyright 2021. MIT License.
# Ref: https://github.com/pressly/goose/blob/master/install.sh
#
# Adapted from the Deno installer: Copyright 2019 the Deno authors. All rights reserved. MIT license.
# Ref: https://github.com/denoland/deno_install
#
# TODO(everyone): Keep this script simple and easily auditable.

# Not intended for Windows.

set -e

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)

if [ "$arch" = "aarch64" ]; then
	arch="arm64"
fi

if [ $# -eq 0 ]; then
	savvy_uri="https://github.com/getsavvyinc/savvy-cli/releases/latest/download/savvy_${os}_${arch}"
else
	savvy_uri="https://github.com/getsavvyinc/savvy-cli/releases/download/${1}/savvy_${os}_${arch}"
fi

savvy_install="${SAVVY_INSTALL:-/usr/local}"
bin_dir="${savvy_install}/bin"
exe="${bin_dir}/savvy"

if [ ! -d "${bin_dir}" ]; then
	mkdir -p "${bin_dir}"
fi

curl --silent --show-error --location --fail --location --output "${exe}" "$savvy_uri"
chmod +x "${exe}"

echo "savvy was installed successfully to ${exe}"
if command -v savvy >/dev/null; then
	echo "Run 'savvy help' to get started"
fi

echo
echo "Stuck? Join our Discord https://getsavvy.so/discord"

