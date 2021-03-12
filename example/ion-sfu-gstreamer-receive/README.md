# gstreamer-receive
ion-sfu-gstreamer-receive is a simple application that shows how to receive media using ion-sdk-go and subscribe streams from ion-sfu using GStreamer.

## Instructions
### Install GStreamer
This example requires you have GStreamer installed, these are the supported platforms
#### Debian/Ubuntu
`sudo apt-get install libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev gstreamer1.0-plugins-good`
#### Windows MinGW64/MSYS2
`pacman -S mingw-w64-x86_64-gstreamer mingw-w64-x86_64-gst-libav mingw-w64-x86_64-gst-plugins-good mingw-w64-x86_64-gst-plugins-bad mingw-w64-x86_64-gst-plugins-ugly`
#### macOS
` brew install gst-plugins-good pkg-config && export PKG_CONFIG_PATH="/usr/local/opt/libffi/lib/pkgconfig`

### Download ion-sfu-gstreamer-receive
```
export GO111MODULE=on
go get github.com/pion/ion-sdk-go/example/ion-sfu-gstreamer-receive
```

### Run ion-sfu-gstreamer-receive
```
ion-sfu-gstreamer-receive -addr "127.0.0.1:50051" -session "test room"
```
