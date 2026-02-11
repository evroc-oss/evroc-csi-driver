#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 evroc
#
# Automated version bump script for evroc CSI driver
# Usage: ./scripts/bump-version.sh <version>
#        ./scripts/bump-version.sh v0.2.0
#        ./scripts/bump-version.sh 0.2.0

set -e

if [ -z "$1" ]; then
    echo "Error: Version number required"
    echo "Usage: ./scripts/bump-version.sh <version>"
    echo "Example: ./scripts/bump-version.sh v0.2.0"
    exit 1
fi

INPUT_VERSION="$1"
CHART_FILE="chart/Chart.yaml"
VALUES_FILE="chart/values.yaml"
VERSION_FILE="VERSION"
CHANGELOG_FILE="CHANGELOG.md"

# Strip 'v' prefix if present
NEW_VERSION="${INPUT_VERSION#v}"

# Validate version format (semantic versioning)
if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Invalid version format '$INPUT_VERSION'"
    echo "Version must be in format: [v]major.minor.patch (e.g., v0.2.0 or 0.2.0)"
    exit 1
fi

# Get current version from Chart.yaml
CURRENT_VERSION=$(grep "^version:" "$CHART_FILE" | awk '{print $2}')

if [ -z "$CURRENT_VERSION" ]; then
    echo "Error: Could not find current version in $CHART_FILE"
    exit 1
fi

echo "Current version: $CURRENT_VERSION"
echo "New version: $NEW_VERSION"

# Update Chart.yaml
echo "Updating $CHART_FILE..."
sed -i "s/^version: .*/version: $NEW_VERSION/" "$CHART_FILE"
sed -i "s/^appVersion: .*/appVersion: \"$NEW_VERSION\"/" "$CHART_FILE"

# Update values.yaml image tags (only evroc-csi-driver, not sidecars)
echo "Updating $VALUES_FILE..."
# Update all evroc-csi-driver image tags (appears twice: controller and node)
# This uses a multi-line approach: find the repository line, then update the next tag line
sed -i '/repository: ghcr\.io\/evroc-oss\/evroc-csi-driver/{n;s/tag: v[0-9]*\.[0-9]*\.[0-9]*/tag: v'"$NEW_VERSION"'/;}' "$VALUES_FILE"

# Update VERSION file if it exists
if [ -f "$VERSION_FILE" ]; then
    echo "Updating $VERSION_FILE..."
    echo "$NEW_VERSION" > "$VERSION_FILE"
fi

# Update CHANGELOG.md - replace [Unreleased] with the new version
if [ -f "$CHANGELOG_FILE" ]; then
    echo "Updating $CHANGELOG_FILE..."
    TODAY=$(date +%Y-%m-%d)
    sed -i "s/## \[Unreleased\]/## [$NEW_VERSION] - $TODAY/" "$CHANGELOG_FILE"

    # Add [Unreleased] link at the bottom if it doesn't exist
    if ! grep -q "\[Unreleased\]:" "$CHANGELOG_FILE"; then
        echo "" >> "$CHANGELOG_FILE"
        echo "[Unreleased]: https://github.com/evroc-oss/evroc-csi-driver/compare/v$NEW_VERSION...HEAD" >> "$CHANGELOG_FILE"
    fi

    # Add version link at the bottom if it doesn't exist
    if ! grep -q "\[$NEW_VERSION\]:" "$CHANGELOG_FILE"; then
        echo "[$NEW_VERSION]: https://github.com/evroc-oss/evroc-csi-driver/releases/tag/v$NEW_VERSION" >> "$CHANGELOG_FILE"
    fi
fi

# Stage and commit
echo "Staging changes..."
git add "$CHART_FILE"
git add "$VALUES_FILE"
[ -f "$VERSION_FILE" ] && git add "$VERSION_FILE"
[ -f "$CHANGELOG_FILE" ] && git add "$CHANGELOG_FILE"

echo "Creating commit..."
git commit -m "chore: bump version to v$NEW_VERSION"

# Create tag
echo "Creating tag v$NEW_VERSION..."
git tag "v$NEW_VERSION"

echo ""
echo "✅ Version bumped successfully!"
echo "   Old: v$CURRENT_VERSION"
echo "   New: v$NEW_VERSION"
echo ""
echo "Files updated:"
echo "   - $CHART_FILE"
echo "   - $VALUES_FILE"
[ -f "$VERSION_FILE" ] && echo "   - $VERSION_FILE"
[ -f "$CHANGELOG_FILE" ] && echo "   - $CHANGELOG_FILE"
echo ""
echo "To push to remote, run:"
echo "   git push origin $(git branch --show-current)"
echo "   git push origin v$NEW_VERSION"
