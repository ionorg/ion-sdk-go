# Gstreamer send

Gstreamer-send is a simple application that shows how to send video to your `ion-sfu` using `ion-sdk-go` and `GStreamer`.

## Install GStreamer

This example requires you have GStreamer installed, these are the supported platforms

### Debian/Ubuntu

`sudo apt-get install libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev gstreamer1.0-plugins-good`

### Windows MinGW64/MSYS2

`pacman -S mingw-w64-x86_64-gstreamer mingw-w64-x86_64-gst-libav mingw-w64-x86_64-gst-plugins-good mingw-w64-x86_64-gst-plugins-bad mingw-w64-x86_64-gst-plugins-ugly`

### macOS

` brew install gst-plugins-good gst-plugins-ugly pkg-config && export PKG_CONFIG_PATH="/usr/local/opt/libffi/lib/pkgconfig"`

## Run the application

```
go run main.go -addr "localhost:50051" -session "test session"
```

## Customizing your video or audio

`Gstreamer send` also accepts the command line arguments `-video-src` and `-audio-src` allowing you to provide custom inputs.

When prototyping with GStreamer it is highly recommended that you enable debug output, this is done by setting the `GST_DEBUG` enviroment variable.
You can read about that [here](https://gstreamer.freedesktop.org/data/doc/gstreamer/head/gstreamer/html/gst-running.html) a good default value is `GST_DEBUG=*:3`

You can also prototype a GStreamer pipeline by using `gst-launch-1.0` to see how things look before trying them with `gstreamer-send` for the examples below you
also may need additional setup to enable extra video codecs like H264. The output from GST_DEBUG should give you hints

These pipelines work on Linux, they may have issues on other platforms. We would love PRs for more example pipelines that people find helpful!

* a webcam, with computer generated audio.

  `go run main.go -addr "localhost:50051" -session "test session" -video-src "autovideosrc ! video/x-raw, width=320, height=240 ! videoconvert ! queue"`

* a pre-recorded video, sintel.mkv is available [here](https://durian.blender.org/download/)

  `go run main.go -addr "localhost:50051" -session "test session" -video-src "uridecodebin uri=file:///tmp/sintel.mkv ! videoscale ! video/x-raw, width=320, height=240 ! queue " -audio-src "uridecodebin uri=file:///tmp/sintel.mkv ! queue ! audioconvert"`