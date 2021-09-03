package main

import (
	"flag"
	"fmt"
	"runtime"
	"strings"
	"time"

	ilog "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	gst "github.com/pion/ion-sdk-go/pkg/gstreamer-sink"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

var (
	log = ilog.NewLogger(ilog.InfoLevel, "")
	sid = "ion"
	uid = sdk.RandomKey(6)
)

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func main() {
	// parse flag
	var session, addr, cafile string
	var ssl bool
	flag.StringVar(&addr, "addr", "localhost:5551", "ion-cluster grpc addr")
	flag.StringVar(&session, "session", sid, "join session name")
	flag.BoolVar(&ssl, "ssl", false, "use ssl or not")
	flag.StringVar(&cafile, "cafile", "", "ssl ca file name")
	flag.Parse()

	connector := sdk.NewConnector(addr)

	room := sdk.NewRoom(connector)

	// THIS IS ROOM MANAGEMENT API
	// ==========================
	// create room
	err := room.CreateRoom(sdk.RoomInfo{Sid: session})
	if err != nil {
		log.Errorf("error:%v", err)
		return
	}

	// add peer to room
	err = room.AddPeer(sdk.PeerInfo{Sid: session, Uid: uid})
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	// get peers from room
	peers := room.GetPeers(session)
	log.Infof("peers=%+v", peers)

	// update peer in room
	err = room.UpdatePeer(sdk.PeerInfo{Sid: session, Uid: uid, DisplayName: "name"})
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	time.Sleep(3 * time.Second)

	// remove peer from room
	err = room.RemovePeer(session, uid)
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	// lock room
	err = room.UpdateRoom(sdk.RoomInfo{Sid: session, Lock: true})
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	// unlock room
	err = room.UpdateRoom(sdk.RoomInfo{Sid: session, Lock: false})
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	// end room
	err = room.EndRoom(session, "conference end", true)
	if err != nil {
		log.Errorf("error: %v", err)
		return
	}

	// THIS IS ROOM SINGAL API
	// ===============================
	room.OnJoin = func(success bool, info sdk.RoomInfo, err error) {
		log.Infof("OnJoin success = %v, info = %v, err = %v", success, info, err)
		rtc := sdk.NewRTC(connector)
		// subscribe rtp from sessoin
		// comment this if you don't need save to file
		rtc.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
			go func() {
				ticker := time.NewTicker(time.Second * 3)
				for range ticker.C {
					rtcpSendErr := rtc.GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
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

		rtc.OnDataChannel = func(dc *webrtc.DataChannel) {
			log.Infof("dc: %v", dc.Label())
		}

		rtc.OnError = func(err error) {
			log.Errorf("err: %v", err)
		}

		err = rtc.Join(info.Sid)
		if err != nil {
			log.Errorf("error: %v", err)
			return
		}
	}

	room.OnLeave = func(success bool, err error) {
		log.Infof("OnLeave success = %v err = %v", success, err)
	}

	room.OnPeerEvent = func(state sdk.PeerState, peer sdk.PeerInfo) {
		log.Infof("OnPeerEvent state = %v, peer = %v", state, peer)
	}

	room.OnMessage = func(from string, to string, data map[string]interface{}) {
		log.Infof("OnMessage from = %v, to = %v, data = %v", from, to, data)
	}

	room.OnDisconnect = func(sid, reason string) {
		log.Infof("OnDisconnect sid = %v, reason = %v", sid, reason)
	}

	room.OnRoomInfo = func(info sdk.RoomInfo) {
		log.Infof("OnRoomInfo info=%v", info)
	}

	// join room
	err = room.Join(
		sdk.JoinInfo{
			Sid:         sid,
			Uid:         uid,
			DisplayName: uid,
			Role:        sdk.Role_Host,
			Protocol:    sdk.Protocol_WebRTC,
			Direction:   sdk.Peer_BILATERAL,
		},
	)

	if err != nil {
		log.Errorf("Join error: %v", err)
		return
	}

	gst.StartMainLoop()
}
