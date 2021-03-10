# Track to gstreamer

This example will take an audio and video track (ONLY TESTED WITH ONE) and send it to a gstreamer pipeline


## Quick Start

### 0) dependencies

see: [gstreamer-recv install notes](https://github.com/pion/example-webrtc-applications/tree/fbf43a5b96fe966de4ef02daff2124cdb7bbf5b1/gstreamer-receive)

### 1) start ion-sfu with 1 feed in it:

in ion-sfu directory, start ion-sfu allrpc:

```
go run cmd/signal/allrpc/main.go -jaddr :7000 -gaddr :50051
```

run pubsubtest in your browser:
```
firefox examples/pubsubtest/index.html
```

join with webcamera to add an audio/video track to "test session"

### 2) edit main.go to add your rtmp url

sorry this is only necessary while i am still building

### 3) start the program

```
go run main.go  -gaddr "localhost:500051" -session 'test session'
```