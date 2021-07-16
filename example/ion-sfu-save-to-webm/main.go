package main

import (
	"flag"
	"fmt"
	"github.com/lucsky/cuid"
	avp "github.com/pion/ion-avp/pkg"
	"github.com/pion/ion-avp/pkg/elements"
	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"os"
	"os/signal"
	"path"
	"sync"
	"time"
)

func trackToDisk(track *webrtc.TrackRemote, saver avp.Element, client *sdk.Client) {
	log.Infof("got track %v type %v", track.ID(), track.Kind())

	builder := avp.MustBuilder(avp.NewBuilder(track, uint16(100), avp.WithMaxLateTime(time.Millisecond * time.Duration(0))))

	if track.Kind() == webrtc.RTPCodecTypeVideo {
		err := client.GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{SenderSSRC: uint32(track.SSRC()), MediaSSRC: uint32(track.SSRC())}})
		if err != nil {
			log.Errorf("error writing pli %s", err)
		}
	}

	builder.AttachElement(saver)
	go writePliPackets(client, track)

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

func writePliPackets(client *sdk.Client, track *webrtc.TrackRemote) {
	ticker := time.NewTicker(time.Second * 3)
	for range ticker.C {
		err := client.GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{SenderSSRC: uint32(track.SSRC()), MediaSSRC: uint32(track.SSRC())}})
		if err != nil {
			log.Errorf("error writing pli %s", err)
		}
	}
}

func main() {
	// parse flag
	var session, addr string
	flag.StringVar(&addr, "addr", "localhost:50051", "ion-cluster grpc addr")
	flag.StringVar(&session, "session", "test room", "join session name")
	flag.Parse()

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
	engine := sdk.NewEngine(config)

	// create a new client from engine
	cid := fmt.Sprintf("%s_tracktodisk_%s", session, cuid.New())
	c, err := sdk.NewClient(engine, addr, cid)
	if err != nil {
		log.Errorf("sdk.NewClient: err=%v", err)
		return
	}

	// Create new Webm saver
	var onceTrackAudio sync.Once
	var onceTrackVideo sync.Once
	saver := createWebmSaver(session, cid)
	defer saver.Close()

	// Save the audio and video from the tracks
	c.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			onceTrackAudio.Do(func() {
				trackToDisk(track, saver, c)
			})
		}
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			onceTrackVideo.Do(func() {
				trackToDisk(track, saver, c)
			})
		}
	}

	// client join a session
	err = c.Join(session, nil)

	if err != nil {
		log.Errorf("err=%v", err)
	}

	// Close peer connection on exit
	defer c.GetPubTransport().GetPeerConnection().Close()

	closed := make(chan os.Signal, 1)
	signal.Notify(closed, os.Interrupt)
	<-closed
}
