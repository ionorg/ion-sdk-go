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
	info = map[string]interface{}{"name": "room-client"}
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

	connector.OnJoin = func(success bool, info sdk.RoomInfo, err error) {
		log.Infof("OnJoin success = %v, info = %v, err = %v", success, info, err)
	}

	connector.OnLeave = func(success bool, err error) {
		log.Infof("OnLeave success = %v err = %v", success, err)
	}

	connector.OnPeerEvent = func(state sdk.PeerState, peer sdk.PeerInfo) {
		log.Infof("OnPeerEvent state = %v, peer = %v", state, peer)
	}

	connector.OnMessage = func(from string, to string, data map[string]interface{}) {
		log.Infof("OnMessage from = %v, to = %v, data = %v", from, to, data)
	}

	connector.OnDisconnect = func(sid, reason string) {
		log.Infof("OnDisconnect sid = %v, reason = %v", sid, reason)
	}

	connector.OnRoomInfo = func(info sdk.RoomInfo) {
		log.Infof("OnRoomInfo info=%v", info)
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

	// try create room
	err := connector.CreateRoom(sdk.RoomInfo{Sid: session})
	if err != nil {
		log.Error("err=%v", err)
		return
	}

	// join room
	// err = connector.Join(session)
	// if err != nil {
	// 	log.Error("err=%v", err)
	// 	return
	// }

	// add peer
	connector.AddPeer(sdk.PeerInfo{Sid: session, Uid: "peer1"})

	// get peers
	peers := connector.GetPeers(session)
	log.Infof(" peers=%+v", peers)

	// update peer
	connector.UpdatePeer(sdk.PeerInfo{Sid: session, Uid: "peer1", DisplayName: "name"})

	time.Sleep(3 * time.Second)
	// remove peer
	connector.RemovePeer(session, "peer1")

	time.Sleep(3 * time.Second)
	// lock room
	connector.UpdateRoom(sdk.RoomInfo{Sid: session, Lock: true})

	time.Sleep(2 * time.Second)
	// unlock room
	connector.UpdateRoom(sdk.RoomInfo{Sid: session, Lock: false})

	time.Sleep(2 * time.Second)
	// end room
	connector.EndRoom(session, "conference end", true)

	gst.StartMainLoop()
}
