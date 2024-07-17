#! /bin/bash
# run from project root

hashes=""

function build() {
    GOOS=$1
    GOARCH=$2
    # package=$3

    # omitting GOARCH from output name
    dist="$GOOS"
    if [ $GOOS = "windows" ]; then
      dist+=".exe"
    fi
    full_output_path="bin/all/mkcert_$4-$dist"


    echo "Building for '$GOOS $GOARCH' to '$full_output_path'"
    env GOOS="$GOOS" GOARCH="$GOARCH" go build -o "$full_output_path" -ldflags "-X main.Version=$(git describe --tags)"
    ln -f "$full_output_path" "bin/mkcert-$dist"
    cd bin || return
    hashes+=$(shasum -a 256 mkcert-"$dist")"\n"
    cd .. || return
}

function build_all() {
    build darwin  arm64 "$1" "$2"
    build linux   amd64 "$1" "$2"
    build windows amd64 "$1" "$2"
    echo ""
    echo "SHA-256 file hashes:"
    echo -e "$hashes"
}


build_all main.go v1.5.1