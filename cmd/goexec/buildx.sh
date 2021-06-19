#!/bin/sh

# https://github.com/mattn/go-sqlite3/issues/303
# apt-get install gcc-multilib -y
# apt-get install gcc-mingw-w64 -y

if [ -f .runner_env.txt ]
then
    cd yaci/gohelper/cmd/goexec || exit 1
fi

if [ ! -f main.go ]
then
    echo "main.go not found" && exit 1
fi

# GOOS=linux GOARCH=amd64 go build $@ main.go && mv main linuxkeeper
OPT="-ldflags -H=windowsgui"
# GOOS=windows GOARCH=amd64 go build $@ $OPT main.go && cp main.exe ../ci_timer.exe && \

rm -f main main.exe ../../../../goexec.exe ../../../../linuxexecutor

gofiles="main.go"

export CGO_ENABLED=1

echo "build linux version"
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build $1 $2 $3 $4 $5 $5 $gofiles && mv main ../../../../linuxexecutor || exit 1

export CC=x86_64-w64-mingw32-gcc
# export CCXX=i686-w64-mingw32-g++
# export CC_FOR_windows_amd64=i686-w64-mingw32-gcc
# export CXX_FOR_windows_amd64=i686-w64-mingw32-g++

export CGO_ENABLED=1
# export CXX_FOR_TARGET=i686-w64-mingw32-g++
# export CC_FOR_TARGET=i686-w64-mingw32-gcc
# export GOGCCFLAGS=""

echo "build windows version"
GOOS=windows GOARCH=amd64 go build $1 $2 $3 $4 $5 $5 $gofiles && mv main.exe ../../../../goexec.exe || exit 1
git status


