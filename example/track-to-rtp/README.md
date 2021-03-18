# Track to RTP (or FFMpeg, gstreamer, etc)

This example will take an audio and video track (ONLY TESTED WITH ONE) and replay it with RTP on local UDP ports 4000 (audio track) and 4002 (video)


## Quick Start

### 1 start ion-sfu allrpc, and run pubsubtest in your browser; join with webcamera to add an audio/video track to "test session"

### 2 run the RTP forwarder

```
go run main.go -addr "localhost:50051" -session "test session"
```

### 3 consume the RTP feed

watch it locally:
```
ffplay -protocol_whitelist file,udp,rtp -i subscribe.sdp
```

broadcast to RTMP:
```
# set your STREAM_KEY and RTMP_URL
export RTMP_URL=rtmp://live.twitch.tv/app/$STREAM_KEY

ffmpeg -protocol_whitelist file,udp,rtp -i subscribe.sdp -c:v libx264 -preset veryfast -b:v 3000k -maxrate 3000k -bufsize 6000k -pix_fmt yuv420p -g 50 -c:a aac -b:a 160k -ac 2 -ar 44100 -f flv $RTMP_URL
```
