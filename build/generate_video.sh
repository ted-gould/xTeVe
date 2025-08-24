#!/bin/bash

# Path to the blank image and output video
BLANK_IMAGE="docs/images/x_transparent.png"
OUTPUT_VIDEO="src/html/video/stream-limit.bin"
mkdir -p src/html/video

if ! command -v ffmpeg &> /dev/null
then
    echo "ffmpeg could not be found, skipping video generation."
    exit 0
fi

# Generate a 1-second video from a blank image using ffmpeg
ffmpeg -y -loop 1 -i "$BLANK_IMAGE" -c:v libx264 -t 1 -pix_fmt yuv420p -vf "scale=1920:1080" "$OUTPUT_VIDEO"
