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
		log.Errorf("error: %v", err)
		fileWriter.Close()
		return
	}

	for {
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			log.Warnf("track.ReadRTP error: %v", err)
			break
		}
		if err := fileWriter.WriteRTP(rtpPacket); err != nil {
			log.Warnf("fileWriter.WriteRTP error: %v", err)
			break
		}
	}
}

func main() {

	// parse flag
	var session, addr, file, logLevel string
	flag.StringVar(&file, "file", "", "Path to the file media")
	flag.StringVar(&addr, "addr", "localhost:5551", "Ion-sfu grpc addr")
	flag.StringVar(&session, "session", "ion", "join session name")
	flag.StringVar(&logLevel, "log", "info", "log level")
	flag.Parse()

	log.Init(logLevel)

	rtc := sdk.NewRTC()
	connector := sdk.NewConnector(addr)
	signaller, err := connector.Signal(rtc)
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}
	rtc.Start(signaller)

	// user define receiving rtp
	rtc.OnTrack = saveToDisk

	rtc.OnDataChannel = func(dc *webrtc.DataChannel) {
		log.Infof("dc: %v", dc.Label())
	}

	rtc.OnError = func(err error) {
		log.Errorf("err: %v", err)
	}

	err = rtc.Join(session, sdk.RandomKey(4))
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	// publish file to session if needed
	err = rtc.PublishFile(file, true, true)
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	select {}
}
