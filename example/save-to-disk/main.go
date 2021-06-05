package main

import (
	"flag"
	"fmt"
	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"strings"
	"time"
)

const (
	audioFileName = "output.ogg"
	videoFileName = "output.ivf"
)

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
	c, err := sdk.NewClient(engine, addr, "")
	if err != nil {
		log.Errorf("sdk.NewClient: err=%v", err)
		return
	}

	c.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Println("Got track")
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

		oggFile, err := oggwriter.New(audioFileName, 48000, 2)
		if err != nil {
			panic(err)
		}
		defer oggFile.Close()

		ivfFile, err := ivfwriter.New(videoFileName)
		if err != nil {
			panic(err)
		}
		defer ivfFile.Close()

		codecName := strings.Split(track.Codec().RTPCodecCapability.MimeType, "/")[1]
		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), codecName)
		buf := make([]byte, 1400)
		rtpPacket := &rtp.Packet{}
		for {
			n, _, readErr := track.Read(buf)
			if readErr != nil {
				log.Errorf("%v", readErr)
				return
			}

			if err = rtpPacket.Unmarshal(buf[:n]); err != nil {
				panic(err)
			}

			if codecName == "opus" {
				log.Debugf("Got Opus track, saving to disk as output.opus (48 kHz, 2 channels)")

				if err := oggFile.WriteRTP(rtpPacket); err != nil {
					log.Panicf("Error write ogg: ", err)
				}
			} else if codecName == "vp8" {
				log.Debugf("Got VP8 track, saving to disk as output.ivf")

				if len(rtpPacket.Payload) < 4 {
					log.Debugf("Ignore packet: payload is not large enough to ivf container header, %v\n", rtpPacket)
					continue
				}

				if err := ivfFile.WriteRTP(rtpPacket); err != nil {
					log.Panicf("Error write ivf: ", err)
				}
			}

		}
	}

	// client join a session
	err = c.Join(session, nil)

	// publish file to session if needed
	if err != nil {
		log.Errorf("err=%v", err)
	}

	select {}
}
