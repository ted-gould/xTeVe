#!/bin/bash

set -e

if ! command -v ffmpeg &> /dev/null
then
    echo "Unable to find 'ffmpeg' please install with 'sudo apt-get update && sudo apt-get install ffmpeg'" >&2
    exit 1
fi

mkdir -p src/html/video
ffmpeg -y -loop 1 -i src/html/img/stream-limit.jpg -c:v libx264 -t 1 -pix_fmt yuv420p -vf scale=1920:1080 -f mpegts src/html/video/stream-limit.bin
