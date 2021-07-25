package main

import (
	"flag"

	gst "github.com/pion/ion-sdk-go/pkg/gstreamer-src"

	ilog "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
)

var ()

func main() {
	// parse flag
	var session, addr, logLevel string
	flag.StringVar(&addr, "addr", "localhost:5551", "Ion-sfu grpc addr")
	flag.StringVar(&session, "session", "ion", "join session name")
	flag.StringVar(&logLevel, "log", "info", "log level:debug|info|warn|error")
	audioSrc := flag.String("audio-src", "audiotestsrc", "GStreamer audio src")
	videoSrc := flag.String("video-src", "videotestsrc", "GStreamer video src")
	flag.Parse()

	log := ilog.NewLogger(ilog.StringToLevel(logLevel), "main")

	// add stun servers
	webrtcCfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			webrtc.ICEServer{
				URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
			},
		},
	}

	config := sdk.Config{
		LogLevel: logLevel,
		WebRTC: sdk.WebRTCTransportConfig{
			Configuration: webrtcCfg,
		},
	}
	// new sdk engine
	e := sdk.NewEngine(config)

	// get a client from engine
	c, err := sdk.NewClient(e, addr, "client id")
	if err != nil {
		log.Errorf("sdk.NewClient error : %v", err)
		return
	}

	c.OnTrackEvent = func(event sdk.TrackEvent) {
		log.Infof("OnTrackEvent: %+v", event)
		if event.State == sdk.TrackAdd {
			var trackIds []string
			for _, track := range event.Tracks {
				trackIds = append(trackIds, track.ID)
			}
			err := c.Subscribe(trackIds, true)
			if err != nil {
				log.Errorf("Subscribe trackIds=%v error: %v", trackIds, err)
			}
		}
	}

	peerConnection := c.GetPubTransport().GetPeerConnection()

	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Infof("Connection state changed: %s", state)
	})

	if err != nil {
		log.Errorf("client err=%v", err)
		panic(err)
	}

	err = e.AddClient(c)
	if err != nil {
		return
	}

	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "video/vp8"}, "video", "pion2")
	if err != nil {
		panic(err)
	}

	_, err = peerConnection.AddTrack(videoTrack)
	if err != nil {
		panic(err)
	}

	audioTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "audio/opus"}, "audio", "pion1")
	if err != nil {
		panic(err)
	}
	_, err = peerConnection.AddTrack(audioTrack)
	if err != nil {
		panic(err)
	}

	// client join a session
	err = c.Join(session, nil)

	if err != nil {
		log.Errorf("join err=%v", err)
		panic(err)
	}

	// Start pushing buffers on these tracks
	gst.CreatePipeline("opus", []*webrtc.TrackLocalStaticSample{audioTrack}, *audioSrc).Start()
	gst.CreatePipeline("vp8", []*webrtc.TrackLocalStaticSample{videoTrack}, *videoSrc).Start()

	select {}
}
