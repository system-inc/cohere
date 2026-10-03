#!/bin/bash
# Packages the VS Code extension as a .vsix, versioned with the cohere release it ships beside.
#
#     package-vscode-extension.sh <version> <output-directory>
#     package-vscode-extension.sh 1.0.0 dist
#
# The extension is versioned in lockstep with cohere, 1.0.0 with 1.0.0, so the version is stamped here
# from the release's own rather than kept in step by hand in editors/vscode/package.json. It packages a
# copy, so the checkout is never written to.
#
# No LICENSE is chosen yet (#m5wmxnk), so the package is made with --skip-license, which vsce otherwise
# asks about interactively and a CI runner cannot answer.
set -euo pipefail

version=$1
output=$(mkdir -p "$2" && cd "$2" && pwd)
source=$(cd "$(dirname "$0")/../../editors/vscode" && pwd)
vsix="$output/cohere-$version.vsix"

stage=$(mktemp -d)
cp -R "$source/." "$stage/"
cd "$stage"
npm pkg set version="$version"
npx --yes @vscode/vsce@4.0.0 package --no-dependencies --skip-license --out "$vsix"

# Proved from the package itself rather than from the stage: what ships is what the .vsix holds.
packaged=$(unzip -p "$vsix" extension/package.json | node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(0, "utf8")).version)')
if [ "$packaged" != "$version" ]; then
    echo "the .vsix says version $packaged, and this release is $version"
    exit 1
fi
listing=$(unzip -l "$vsix")
for required in extension/extension.js extension/cohere-format.js extension/icon.png extension/readme.md; do
    if ! grep -qi " $required\$" <<< "$listing"; then
        echo "the .vsix holds no $required"
        exit 1
    fi
done
if grep -q "cohere-format.test.js" <<< "$listing"; then
    echo "the .vsix ships the tests, which .vscodeignore leaves out"
    exit 1
fi
echo "packaged $vsix, version $packaged"
