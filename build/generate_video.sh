#!/bin/bash

if ! [ -x "$(command -v ffmpeg)" ]; then
  echo 'Error: ffmpeg is not installed. Please install ffmpeg and run `make build` or `make test`.' >&2
  exit 1
fi

# Create a blank video file for browsers that require a video stream to be present
# This is used for the stream limit feature
ffmpeg -f lavfi -i color=c=black:s=1280x720:r=5 -t 1 -pix_fmt yuv420p -y -f mpegts src/html/video/stream-limit.bin
