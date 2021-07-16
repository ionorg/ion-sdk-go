# Save to webm

This example will take an audio and video track and save it into a local webm file.

## Quick Start

1. Start ion-sfu allrpc, and run [pubsubtest example](https://github.com/pion/ion-sfu/tree/master/examples/pubsubtest) in your browser and you join with your web camera to add an audio/video track to the "test session" room.

2. Run the script

```
go run main.go -addr "localhost:50051" -session "test session"
```

3. Your video or audio track will be saved and can be accessed after quitting the application with `Control + C`
