#!/bin/sh
set -eu

if [ "$(uname -s)" != "Darwin" ]; then
	printf '%s\n' 'This registration script currently supports macOS Edge only.' >&2
	exit 2
fi
if [ "$#" -ne 1 ]; then
	printf '%s\n' 'Usage: register-edge-native-host.sh <32-character-extension-id>' >&2
	exit 2
fi

extension_id=$1
if [ "${#extension_id}" -ne 32 ]; then
	printf '%s\n' 'Extension ID must contain exactly 32 characters.' >&2
	exit 2
fi
case "$extension_id" in
	*[!a-p]*) printf '%s\n' 'Extension ID may contain only letters a through p.' >&2; exit 2 ;;
esac

user_home=${HOME:?HOME must be set}
binary_path="$user_home/.local/bin/d2l-native-host"
manifest_dir="$user_home/Library/Application Support/Microsoft Edge/NativeMessagingHosts"
manifest_path="$manifest_dir/com.sait.d2l.json"
manifest_tmp="$manifest_dir/.com.sait.d2l.$$"
trap 'rm -f "$manifest_tmp"' EXIT HUP INT TERM

if [ ! -f "$binary_path" ] || [ -L "$binary_path" ] || [ ! -x "$binary_path" ]; then
	printf '%s\n' 'Install the managed Chrome native host first.' >&2
	exit 2
fi
if ! go version -m "$binary_path" 2>/dev/null | awk -v id="$extension_id" '
	$1 == "path" && $2 == "d2l-cli" { has_path = 1 }
	index($0, "chrome-extension://" id "/") > 0 { has_origin = 1 }
	END { exit !(has_path && has_origin) }
'; then
	printf '%s\n' 'The installed native host is not managed by this project or is bound to a different extension ID.' >&2
	exit 2
fi

mkdir -p "$manifest_dir"
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
if [ -L "$manifest_path" ] || { [ -e "$manifest_path" ] && ! cmp -s "$manifest_tmp" "$manifest_path"; }; then
	printf 'Refusing to overwrite existing Edge host registration: %s\n' "$manifest_path" >&2
	exit 2
fi
if [ ! -e "$manifest_path" ]; then
	mv "$manifest_tmp" "$manifest_path"
fi
printf 'Registered Edge host manifest: %s\n' "$manifest_path"
printf '%s\n' 'Restart Edge, then use the extension’s Test Native Messaging action.'
