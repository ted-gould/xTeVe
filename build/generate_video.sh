#!/bin/bash
set -e
mkdir -p src/html/video
ffmpeg -y -loop 1 -i src/html/img/stream-limit.jpg -c:v libx264 -t 1 -pix_fmt yuv420p -vf scale=1920:1080 src/html/video/stream-limit.bin
