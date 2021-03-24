package main

import (
	"flag"
	"fmt"
	"runtime"
	"strings"
	"time"

	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	gst "github.com/pion/ion-sdk-go/pkg/gstreamer-sink"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func runClientLoop(addr, session string) {

	// add stun servers
	webrtcCfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			webrtc.ICEServer{
				URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
			},
		},
	}

	config := sdk.Config{
		Log: log.Config{
			Level: "debug",
		},
		WebRTC: sdk.WebRTCTransportConfig{
			Configuration: webrtcCfg,
		},
	}
	// new sdk engine
	engine := sdk.NewEngine(config)

	// create a new client from engine
	c, err := sdk.NewClient(engine, addr, "")
	if err != nil {
		log.Errorf("sdk.NewClient: err=%v", err)
		return
	}

	// subscribe rtp from sessoin
	// comment this if you don't need save to file
	c.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		go func() {
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				rtcpSendErr := c.GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				if rtcpSendErr != nil {
					fmt.Println(rtcpSendErr)
				}
			}
		}()

		codecName := strings.Split(track.Codec().RTPCodecCapability.MimeType, "/")[1]
		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), codecName)
		pipeline := gst.CreatePipeline(strings.ToLower(codecName))
		pipeline.Start()
		buf := make([]byte, 1400)
		for {
			i, _, readErr := track.Read(buf)
			if readErr != nil {
				log.Errorf("%v", readErr)
			}

			pipeline.Push(buf[:i])
		}
	}

	// client join a session
	err = c.Join(session)

	// publish file to session if needed
	if err != nil {
		log.Errorf("err=%v", err)
	}

	select {}
}

func main() {
	// init log
	fixByFile := []string{"asm_amd64.s", "proc.go", "icegatherer.go"}
	fixByFunc := []string{"AddProducer", "NewClient"}
	log.Init("debug", fixByFile, fixByFunc)

	// parse flag
	var session, addr string
	flag.StringVar(&addr, "addr", "localhost:50051", "ion-cluster grpc addr")
	flag.StringVar(&session, "session", "test room", "join session name")
	flag.Parse()

	go runClientLoop(addr, session)
	gst.StartMainLoop()
}
