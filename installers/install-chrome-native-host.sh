#!/bin/sh
set -eu

if [ "$(uname -s)" != "Darwin" ]; then
  printf '%s\n' 'This installer currently supports macOS Chrome only.' >&2
  exit 2
fi
if [ "$#" -ne 1 ]; then
  printf '%s\n' 'Usage: install-chrome-native-host.sh <32-character-extension-id>' >&2
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

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
user_home=${HOME:?HOME must be set}
install_dir="$user_home/.local/bin"
binary_path="$install_dir/d2l-native-host"
manifest_dir="$user_home/Library/Application Support/Google/Chrome/NativeMessagingHosts"
manifest_path="$manifest_dir/com.sait.d2l.json"

mkdir -p "$install_dir" "$manifest_dir"
linker_flag="-X main.expectedExtensionOrigin=chrome-extension://$extension_id/"
(cd "$repo_root" && go build -ldflags "$linker_flag" -o "$binary_path" .)
chmod 755 "$binary_path"

json_binary_path=$(printf '%s' "$binary_path" | sed 's/\\/\\\\/g; s/"/\\"/g')
cat > "$manifest_path" <<EOF
{
  "name": "com.sait.d2l",
  "description": "D2L read-only CLI native messaging host",
  "path": "$json_binary_path",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://$extension_id/"]
}
EOF
chmod 644 "$manifest_path"

printf 'Installed native host binary: %s\n' "$binary_path"
printf 'Registered Chrome host manifest: %s\n' "$manifest_path"
printf '%s\n' 'Restart Chrome, then use the extension’s Test Native Messaging action.'
