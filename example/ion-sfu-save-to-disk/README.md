# Save to disk

This example will take an audio and video track and save it into a local file.

## Quick Start

1. Start ion-sfu allrpc, and run [pubsubtest example](https://github.com/pion/ion-sfu/tree/master/examples/pubsubtest) in your browser and you join with your web camera to add an audio/video track to the "test session" room.

2. Run the RTP forwarder

```
go run main.go -addr "localhost:50051" -session "test session"
```

3. The contents of your video or audio stream will be written into the output file. You can also convert the file to another format using FFMPEG.

```
ffmpeg -i output.ivf  -codec copy output.webm
```
