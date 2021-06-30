package main

import (
	"flag"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/lucsky/cuid"
	ilog "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	gst "github.com/pion/ion-sdk-go/pkg/gstreamer-sink"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

var (
	log  = ilog.NewLoggerWithFields(ilog.DebugLevel, "ion-cluster", nil)
	info = map[string]interface{}{"name": "bizclient"}
	uid  = cuid.New()
)

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func main() {
	// parse flag
	var session, addr string
	flag.StringVar(&addr, "addr", "localhost:5551", "ion-cluster grpc addr")
	flag.StringVar(&session, "session", "test room", "join session name")
	flag.Parse()

	connector := sdk.NewIonConnector(addr, uid, info)

	connector.OnError = func(err error) {
		log.Errorf("OnError %v", err)
	}

	connector.OnJoin = func(success bool, reason string) {
		log.Infof("OnJoin success = %v, reason = %v", success, reason)
	}

	connector.OnLeave = func(reason string) {
		log.Infof("OnLeave reason = %v", reason)
	}

	connector.OnPeerEvent = func(event sdk.PeerEvent) {
		log.Infof("OnPeerEvent peer = %v, state = %v", event.Peer, event.State)
	}

	connector.OnStreamEvent = func(event sdk.StreamEvent) {
		log.Infof("StreamEvent state = %v, sid = %v, uid = %v, streams = %v",
			event.State,
			event.Sid,
			event.Uid,
			event.Streams)
	}

	connector.OnMessage = func(msg sdk.Message) {
		log.Infof("OnMessage from = %v, to = %v, msg = %v", msg.From, msg.To, msg.Data)
	}

	// subscribe rtp from sessoin
	// comment this if you don't need save to file
	connector.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		go func() {
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				rtcpSendErr := connector.SFU().GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				if rtcpSendErr != nil {
					fmt.Println(rtcpSendErr)
				}
			}
		}()

		codecName := strings.Split(track.Codec().RTPCodecCapability.MimeType, "/")[1]
		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), codecName)
		pipeline := gst.CreatePipeline(strings.ToLower(codecName))
		pipeline.Start()
		defer pipeline.Stop()
		buf := make([]byte, 1400)
		for {
			i, _, readErr := track.Read(buf)
			if readErr != nil {
				log.Errorf("%v", readErr)
				return
			}

			pipeline.Push(buf[:i])
		}
	}

	err := connector.Join(session)
	if err != nil {
		log.Errorf("join err %v", err)
	}

	gst.StartMainLoop()
}
