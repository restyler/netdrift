#!/bin/bash

# Script to replace upstream_proxies in fineproxy.json with hosts from fineproxy.txt
# Usage: ./update_fineproxy_config.sh

CONFIG_FILE="configs/fineproxy.json"
HOSTS_FILE="configs/fineproxy.txt"
BACKUP_FILE="configs/fineproxy.json.backup"

# Check if files exist
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "Error: $CONFIG_FILE not found"
    exit 1
fi

if [[ ! -f "$HOSTS_FILE" ]]; then
    echo "Error: $HOSTS_FILE not found"
    exit 1
fi

# Create backup
echo "Creating backup: $BACKUP_FILE"
cp "$CONFIG_FILE" "$BACKUP_FILE"

# Create temporary file for the new JSON
TEMP_FILE=$(mktemp)

# Extract hosts from fineproxy.txt (remove line numbers and arrow)
echo "Reading hosts from $HOSTS_FILE..."
HOSTS=$(grep -o '[0-9]\+\.[0-9]\+\.[0-9]\+\.[0-9]\+:[0-9]\+' "$HOSTS_FILE")

# Count hosts
HOST_COUNT=$(echo "$HOSTS" | wc -l)
echo "Found $HOST_COUNT hosts"

# Build the upstream_proxies JSON array
echo "Building new upstream_proxies array..."
UPSTREAM_JSON="["
FIRST=true
while IFS= read -r host; do
    if [[ -n "$host" ]]; then
        if [[ "$FIRST" = true ]]; then
            FIRST=false
        else
            UPSTREAM_JSON+=","
        fi
        UPSTREAM_JSON+="
      {
        \"url\": \"http://$host\",
        \"enabled\": true,
        \"weight\": 1
      }"
    fi
done <<< "$HOSTS"
UPSTREAM_JSON+="
    ]"

# Read the original JSON and replace the upstream_proxies section
echo "Updating configuration..."
python3 << EOF
import json
import sys

# Read original config
with open('$CONFIG_FILE', 'r') as f:
    config = json.load(f)

# Build new upstream_proxies list
hosts = """$HOSTS""".strip().split('\n')
upstream_proxies = []

for host in hosts:
    if host.strip():
        upstream_proxies.append({
            "url": f"http://{host.strip()}",
            "enabled": True,
            "weight": 1
        })

# Replace upstream_proxies in config
config['upstream_proxies'] = upstream_proxies

# Write updated config
with open('$TEMP_FILE', 'w') as f:
    json.dump(config, f, indent=2)

print(f"Updated configuration with {len(upstream_proxies)} upstream proxies")
EOF

# Replace original file with updated version
mv "$TEMP_FILE" "$CONFIG_FILE"

echo "Configuration updated successfully!"
echo "Original backed up to: $BACKUP_FILE"
echo "Updated $HOST_COUNT upstream proxies in $CONFIG_FILE"

# Verify the JSON is valid
if python3 -m json.tool "$CONFIG_FILE" > /dev/null 2>&1; then
    echo "✓ JSON validation passed"
else
    echo "✗ JSON validation failed - restoring backup"
    mv "$BACKUP_FILE" "$CONFIG_FILE"
    exit 1
fi