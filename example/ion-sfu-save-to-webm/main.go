package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"sync"
	"time"

	avp "github.com/pion/ion-avp/pkg"
	"github.com/pion/ion-avp/pkg/elements"
	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

func trackToDisk(track *webrtc.TrackRemote, saver avp.Element, rtc *sdk.RTC) {
	log.Infof("got track %v type %v", track.ID(), track.Kind())

	builder := avp.MustBuilder(avp.NewBuilder(track, uint16(100), avp.WithMaxLateTime(time.Millisecond*time.Duration(0))))

	if track.Kind() == webrtc.RTPCodecTypeVideo {
		err := rtc.GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{SenderSSRC: uint32(track.SSRC()), MediaSSRC: uint32(track.SSRC())}})
		if err != nil {
			log.Errorf("error writing pli %s", err)
		}
	}

	builder.AttachElement(saver)
	go writePliPackets(rtc, track)

	builder.OnStop(func() {
		log.Infof("builder stopped")
	})
}

func createWebmSaver(sid, pid string) avp.Element {
	filewriter := elements.NewFileWriter(
		path.Join("./", fmt.Sprintf("%s-%s.webm", sid, pid)),
		4096,
	)
	webm := elements.NewWebmSaver()
	webm.Attach(filewriter)
	return webm
}

func writePliPackets(rtc *sdk.RTC, track *webrtc.TrackRemote) {
	ticker := time.NewTicker(time.Second * 3)
	for range ticker.C {
		err := rtc.GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{SenderSSRC: uint32(track.SSRC()), MediaSSRC: uint32(track.SSRC())}})
		if err != nil {
			log.Errorf("error writing pli %s", err)
		}
	}
}

func main() {
	// parse flag
	var session, addr string
	flag.StringVar(&addr, "addr", "localhost:5551", "ion-cluster grpc addr")
	flag.StringVar(&session, "session", "ion", "join session name")
	flag.Parse()

	connector := sdk.NewConnector(addr)
	rtc := sdk.NewRTC(connector)

	// Create new Webm saver
	var onceTrackAudio sync.Once
	var onceTrackVideo sync.Once
	saver := createWebmSaver(session, sdk.RandomKey(4))
	defer saver.Close()

	// Save the audio and video from the tracks
	rtc.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			onceTrackAudio.Do(func() {
				trackToDisk(track, saver, rtc)
			})
		}
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			onceTrackVideo.Do(func() {
				trackToDisk(track, saver, rtc)
			})
		}
	}

	// client join a session
	err := rtc.Join(session)

	if err != nil {
		log.Errorf("error: %v", err)
	}

	// Close peer connection on exit
	defer rtc.GetPubTransport().GetPeerConnection().Close()

	closed := make(chan os.Signal, 1)
	signal.Notify(closed, os.Interrupt)
	<-closed
}
