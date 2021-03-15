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

const (
	uid  = "biz-client-id"
	info = `{}`
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
	e := sdk.NewEngine(config)

	// get a client from engine
	c := e.AddClient(addr, session, uid)

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
	err := c.Join(session)

	// publish file to session if needed
	if err != nil {
		log.Errorf("err=%v", err)
	}

	select {}
}

func main() {
	fixByFile := []string{"asm_amd64.s", "proc.go"}
	fixByFunc := []string{}
	log.Init("debug", fixByFile, fixByFunc)

	// parse flag
	var session, addr string
	flag.StringVar(&addr, "addr", "localhost:50051", "ion-cluster grpc addr")
	flag.StringVar(&session, "session", "test room", "join session name")
	flag.Parse()

	bizcli := sdk.NewBizClient(addr)

	bizcli.OnError = func(err error) {
		log.Errorf("OnError %v", err)
	}

	bizcli.OnJoin = func(success bool, reason string) {
		log.Infof("OnJoin success = %v, reason = %v", success, reason)
		if success {
			go runClientLoop(addr, session)
		}
	}

	bizcli.OnLeave = func(reason string) {
		log.Infof("OnLeave reason = %v", reason)
	}

	bizcli.OnPeerEvent = func(state sdk.PeerState, peer sdk.Peer) {
		log.Infof("OnPeerEvent peer = %v, state = %v", peer, state)
	}

	bizcli.OnStreamEvent = func(state sdk.StreamState, sid string, uid string, streams []*sdk.Stream) {
		log.Infof("StreamEvent state = %v, sid = %v, uid = %v, streams = %v",
			state,
			sid,
			uid,
			streams)
	}

	bizcli.OnMessage = func(from string, to string, data []byte) {
		log.Infof("OnMessage from = %v, to = %v, msg = %v", from, to, data)
	}

	err := bizcli.Join(session, uid, []byte(info))
	if err != nil {
		log.Errorf("join err %v", err)
	}

	gst.StartMainLoop()
}
