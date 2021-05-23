package main

import (
	"flag"
	"fmt"
	"strings"

	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
)

func saveToDisk(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	codec := track.Codec()
	var fileWriter media.Writer
	var err error
	if strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus) {
		log.Infof("Got Opus track, saving to disk as ogg (48 kHz, 2 channels)")
		fileWriter, err = oggwriter.New(fmt.Sprintf("%d_%d.ogg", codec.PayloadType, track.SSRC()), 48000, 2)
	} else if strings.EqualFold(codec.MimeType, webrtc.MimeTypeVP8) {
		log.Infof("Got VP8 track, saving to disk as ivf")
		fileWriter, err = ivfwriter.New(fmt.Sprintf("%d_%d.ivf", codec.PayloadType, track.SSRC()))
	}

	if err != nil {
		log.Errorf("err=%v", err)
		fileWriter.Close()
		return
	}

	for {
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			panic(err)
		}
		if err := fileWriter.WriteRTP(rtpPacket); err != nil {
			panic(err)
		}
	}
}

func main() {
	// init log
	log.Init("debug")

	// parse flag
	var session, addr, file string
	flag.StringVar(&file, "file", "", "Path to the file media")
	flag.StringVar(&addr, "addr", "localhost:50051", "Ion-sfu grpc addr")
	flag.StringVar(&session, "session", "test session", "join session name")
	flag.Parse()

	// add stun servers
	webrtcCfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			webrtc.ICEServer{
				URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
			},
		},
	}

	config := sdk.Config{
		WebRTC: sdk.WebRTCTransportConfig{
			Configuration: webrtcCfg,
		},
	}
	// new sdk engine
	e := sdk.NewEngine(config)

	// create a new client from engine
	c, err := sdk.NewClient(e, addr, "client id")
	if err != nil {
		log.Errorf("err=%v", err)
		return
	}
	// subscribe rtp from sessoin
	// comment this if you don't need save to file
	c.OnTrack = saveToDisk

	// client join a session
	err = c.Join(session, nil)
	if err != nil {
		log.Errorf("err=%v", err)
		return
	}

	// publish file to session if needed
	if file != "" {
		err = c.PublishWebm(file, true, true)
		if err != nil {
			log.Errorf("err=%v", err)
			return
		}
	}

	select {}
}
