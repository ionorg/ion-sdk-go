# Join From RTMP - WORK IN PROGRESS, DOES NOT FUNCTION

Thanks to @Sean-Der for this example - https://github.com/Sean-Der/rtmp-to-webrtc

## Quick Start

```
# start ion-sfu with allrpc, open pubsubtest in your browser to view the session

# Start the RTMP server at rtmp://localhost:1935
go run ./main.go

# Publish a test RTMP feed from ffmpeg (must be ALAW audio + h264 video)
gst-launch-1.0 videotestsrc ! video/x-raw,format=I420 ! x264enc speed-preset=ultrafast tune=zerolatency key-int-max=20 ! flvmux name=flvmux ! rtmpsink location=rtmp://localhost:1935/test/session audiotestsrc ! alawenc ! flvmux.

```
