#!/bin/sh
set -eu

if [ "$(uname -s)" != "Darwin" ]; then
  printf '%s\n' 'This installer currently supports macOS Chrome only.' >&2
  exit 2
fi
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	printf '%s\n' 'Usage: install-chrome-native-host.sh <32-character-extension-id> [--upgrade]' >&2
  exit 2
fi

upgrade=false
if [ "$#" -eq 2 ]; then
	if [ "$2" != "--upgrade" ]; then
		printf '%s\n' 'Usage: install-chrome-native-host.sh <32-character-extension-id> [--upgrade]' >&2
		exit 2
	fi
	upgrade=true
fi

extension_id=$1
if [ "${#extension_id}" -ne 32 ]; then
  printf '%s\n' 'Extension ID must contain exactly 32 characters.' >&2
  exit 2
fi
case "$extension_id" in
  *[!a-p]*) printf '%s\n' 'Extension ID may contain only letters a through p.' >&2; exit 2 ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
user_home=${HOME:?HOME must be set}
install_dir="$user_home/.local/bin"
binary_path="$install_dir/d2l-native-host"
cli_path="$install_dir/d2l"
manifest_dir="$user_home/Library/Application Support/Google/Chrome/NativeMessagingHosts"
manifest_path="$manifest_dir/com.sait.d2l.json"
build_tmp="$install_dir/.d2l-native-host.$$"
cli_tmp="$install_dir/.d2l-cli.$$"
manifest_tmp="$manifest_dir/.com.sait.d2l.$$"

cleanup() { rm -f "$build_tmp" "$cli_tmp" "$manifest_tmp"; }
trap cleanup EXIT HUP INT TERM

is_managed_binary() {
	[ -f "$1" ] && [ ! -L "$1" ] || return 1
	go version -m "$1" 2>/dev/null | awk -v id="$extension_id" '
		$1 == "path" && $2 == "d2l-cli" { has_path = 1 }
		index($0, "chrome-extension://" id "/") > 0 { has_origin = 1 }
		END { exit !(has_path && has_origin) }
'
}

if [ -e "$cli_path" ]; then
	if [ "$upgrade" != true ]; then
		printf 'Refusing to overwrite existing CLI: %s\nUse --upgrade only for an existing d2l-cli installation.\n' "$cli_path" >&2
		exit 2
	fi
	if ! is_managed_binary "$cli_path"; then
		printf 'Refusing to upgrade an unrecognized CLI: %s\n' "$cli_path" >&2
		exit 2
	fi
fi
if [ -e "$binary_path" ] && { [ "$upgrade" != true ] || ! is_managed_binary "$binary_path"; }; then
	printf 'Refusing to overwrite an unrecognized native host: %s\n' "$binary_path" >&2
	exit 2
fi
if [ -e "$manifest_path" ] && { [ "$upgrade" != true ] || ! grep -F '"name": "com.sait.d2l"' "$manifest_path" >/dev/null || ! grep -F "chrome-extension://$extension_id/" "$manifest_path" >/dev/null || ! grep -F "$binary_path" "$manifest_path" >/dev/null; }; then
	printf 'Refusing to overwrite an unrecognized native host registration: %s\n' "$manifest_path" >&2
	exit 2
fi

mkdir -p "$install_dir" "$manifest_dir"
linker_flag="-X main.expectedExtensionOrigin=chrome-extension://$extension_id/"
(cd "$repo_root" && go build -ldflags "$linker_flag" -o "$build_tmp" .)
chmod 755 "$build_tmp"
cp "$build_tmp" "$cli_tmp"

if [ "$upgrade" = true ]; then
	backup_stamp=$(date +%Y%m%d-%H%M%S)-$$
	if [ -e "$binary_path" ]; then cp -p "$binary_path" "$binary_path.backup.$backup_stamp"; fi
	if [ -e "$cli_path" ]; then cp -p "$cli_path" "$cli_path.backup.$backup_stamp"; fi
	if [ -e "$manifest_path" ]; then cp -p "$manifest_path" "$manifest_path.backup.$backup_stamp"; fi
fi

mv -f "$build_tmp" "$binary_path"
mv -f "$cli_tmp" "$cli_path"

json_binary_path=$(printf '%s' "$binary_path" | sed 's/\\/\\\\/g; s/"/\\"/g')
cat > "$manifest_tmp" <<EOF
{
  "name": "com.sait.d2l",
  "description": "D2L read-only CLI native messaging host",
  "path": "$json_binary_path",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://$extension_id/"]
}
EOF
chmod 644 "$manifest_tmp"
mv -f "$manifest_tmp" "$manifest_path"

printf 'Installed native host binary: %s\n' "$binary_path"
printf 'Installed CLI binary: %s\n' "$cli_path"
printf 'Registered Chrome host manifest: %s\n' "$manifest_path"
if [ "$upgrade" = true ]; then printf 'Previous managed files kept as .backup.%s\n' "$backup_stamp"; fi
printf '%s\n' 'Restart Chrome, then use the extension’s Test Native Messaging action.'
