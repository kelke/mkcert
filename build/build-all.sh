#! /bin/bash
# run from project root


function build() {
    GOOS=$1
    GOARCH=$2
    package=$3

    # omitting GOARCH from output name
    output_name="bin/all/mkcert-$4_$GOOS"
	if [ $GOOS = "windows" ]; then
		output_name+='.exe'
	fi

    echo "Building for '$GOOS $GOARCH' to '$output_name'"
    env GOOS=$GOOS GOARCH=$GOARCH go build -o $output_name -ldflags "-X main.Version=$(git describe --tags)"
    cp $output_name bin/mkcert-$GOOS
}

function build_all() {
    build linux   amd64 $1 $2
    build darwin  arm64 $1 $2
    build windows amd64 $1 $2
}


build_all main.go v0.2