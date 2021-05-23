package main

import (
	"flag"
	"fmt"
	"sync"

	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
)

var streamLock sync.RWMutex

type clientMap struct {
	client    *sdk.Client
	tracks    []*webrtc.TrackLocalStaticRTP
	recievers []*webrtc.RTPTransceiver
}

var trackMap = make(map[string]clientMap)

var dcLock sync.RWMutex

type dcMap struct {
	client *sdk.Client
	dc     *webrtc.DataChannel
}

var dataMap = make(map[string]dcMap)

func main() {
	// init log
	log.Init("info")

	// parse flag
	var session, session2, addr string
	flag.StringVar(&addr, "addr", "5.9.18.28:50052", "Ion-sfu grpc addr")
	flag.StringVar(&session, "session", "test", "join session name")
	flag.StringVar(&session2, "session2", "test2", "join session name")
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
		Log: log.Config{
			Level: "warn",
		},
		WebRTC: sdk.WebRTCTransportConfig{
			Configuration: webrtcCfg,
		},
	}
	// new sdk engine
	e := sdk.NewEngine(config)

	// create a new client from engine
	c1, err := sdk.NewClient(e, addr, "client id")
	if err != nil {
		log.Errorf("err=%v", err)
		return
	}

	c1.OnDataChannel = func(dc *webrtc.DataChannel) {

		log.Warnf("New DataChannel %s %d\n", dc.Label())
		dcID := fmt.Sprintf("dc %v", dc.Label())
		log.Warnf("DCID %v", dcID)
		client, err := sdk.NewClient(e, addr, dcID)
		if err != nil {
			log.Errorf("err=%v", err)
			return
		}
		dcLock.Lock()
		client.Join(session2)
		dcc, err := client.CreateDataChannel(dc.Label())
		dcc.OnMessage(func(msg webrtc.DataChannelMessage) {
			// bi-directional data channels
			log.Warnf("back msg %v", string(msg.Data))
			dc.SendText(string(msg.Data))
		})
		if err != nil {
			panic(err)
		}
		dataMap[dcID] = dcMap{
			client: client,
			dc:     dcc,
		}
		dcLock.Unlock()

		dc.OnClose(func() {
			dcLock.Lock()
			defer dcLock.Unlock()
			dcID := fmt.Sprintf("closing data channel dc %v", dc.Label())
			dataMap[dcID].dc.Close()
			dataMap[dcID].client.Close()
			delete(dataMap, dcID)
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Warnf("Message from DataChannel %v %v", dc.Label(), string(msg.Data))
			dcLock.Lock()
			defer dcLock.Unlock()
			dcID := fmt.Sprintf("dc %v", dc.Label())
			dataMap[dcID].dc.SendText(string(msg.Data))
		})

	}
	c1.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		streamLock.Lock()
		defer streamLock.Unlock()
		log.Warnf("GOT TRACK id%v mime%v kind %v stream %v", track.ID(), track.Codec().MimeType, track.Kind(), track.StreamID())
		newTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: track.Codec().MimeType}, track.ID(), track.StreamID())
		if err != nil {
			panic(err)
		}
		val, ok := trackMap[track.StreamID()]
		log.Warnf("stream id", track.StreamID(), " ok", ok)
		if ok {
			log.Warnf("adding stream to existing client")
			val.tracks = append(val.tracks, newTrack)

			for _, ctrack := range val.tracks {
				t, err := val.client.Publish(ctrack)
				if err != nil {
					log.Errorf("publish err=%v", err)
					return
				}
				val.recievers = append(val.recievers, t)
			}
			trackMap[track.StreamID()] = val

		} else {
			log.Warnf("creating new client %v", track.StreamID())
			client, err := sdk.NewClient(e, addr, track.StreamID())
			if err != nil {
				log.Errorf("err=%v", err)
				return
			}
			client.Join(session2)
			val.client = client
			val.tracks = append(val.tracks, newTrack)
			trackMap[track.StreamID()] = val
		}

		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		/*
			go func() {
				ticker := time.NewTicker(time.Second * 2)
				for range ticker.C {

					// We need to add direct access to the peerconnection to ion-sdk-go to support PLI here
					// PLI is disabled in this example currently

					if rtcpErr := c1.GetSubTransport().GetPeerConnection().WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}}); rtcpErr != nil {
						fmt.Println(rtcpErr)
					}
				}
			}()
		*/
		go func() {

			for {
				// Read
				rtpPacket, _, err := track.ReadRTP()
				if err != nil {
					streamLock.Lock()
					streamid := track.StreamID()
					log.Warnf("unpublish track", trackMap)
					val, ok := trackMap[streamid]
					if ok {
						log.Warnf("closing client", streamid, len(val.recievers))
						for idx, r := range val.recievers {
							log.Warnf("rid %v, trackid %v", r.Kind().String(), track.Kind().String())
							if r.Kind().String() == track.Kind().String() {
								val.client.UnPublish(r)
								val.recievers = append(val.recievers[:idx], val.recievers[idx+1:]...)
								val.tracks = append(val.tracks[:idx], val.tracks[idx+1:]...)
								break
							}
						}
						log.Warnf("closing client %v", len(val.recievers))
						if len(val.recievers) == 0 {
							val.client.Close()
							val.client = nil
							delete(trackMap, streamid)
						}
					}
					streamLock.Unlock()
					break
					// panic(err)
				}

				if err = newTrack.WriteRTP(rtpPacket); err != nil {
					log.Errorf("track write err", err)
					// break
					// panic(err)
				}
			}
		}()
	}

	err = c1.Join(session)
	if err != nil {
		log.Errorf("err=%v", err)
		return
	}

	select {}
}
