# ion-sdk-go
this is ion golang sdk for ion-sfu
Dependence:
- [x] Gstreamer 2.70.x (for example which use Gstreamer)

Feature:
- [x] Join a session
  - [x] Join with config(NoPublish/NoSubscribe/Relay)
- [x] Subscribe from session
  - [x] OnTrack(user-defined)
- [x] Publish file to session
  - [x] webm
    - [x] vp8+opus
    - [ ] vp9+opus
  - [ ] mp4(h264+opus)
  - [ ] simulcast(publish 3 files)
- [x] Publish rtp to session
  - [x] audio|video only
  - [x] audio codec(opus)
  - [x] video codec
    - [x] vp8
    - [ ] vp9
    - [ ] h264
- [x] Simulcast
  - [x] subscribe
  - [ ] publish
- [x] Publish media device to session
  - [x] camera
  - [x] mic
  - [ ] screen
- [x] Support ion cluster
