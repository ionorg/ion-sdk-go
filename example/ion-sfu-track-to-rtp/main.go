package main

import (
	"flag"
	"fmt"
	"net"
	"time"

	ilog "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"

	//"github.com/pion/rtcp"
	"os/exec"

	"github.com/pion/rtp"
)

var (
	log = ilog.NewLoggerWithFields(ilog.DebugLevel, "", nil)
)

type udpConn struct {
	conn        *net.UDPConn
	port        int
	payloadType uint8
}

func trackToRTP(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	log.Infof("GOT TRACK", track, receiver)

	track_sdp := "track-" + track.ID() + ".sdp"

	cmd := exec.Command("cat", track_sdp)
	output, err := cmd.Output()
	log.Infof("output", track_sdp, output, err)
	// Prepare udp conns
	// Also update incoming packets with expected PayloadType, the browser may use
	// a different value. We have to modify so our stream matches what rtp-forwarder.sdp expects

	var laddr *net.UDPAddr
	if laddr, err = net.ResolveUDPAddr("udp", "127.0.0.1:"); err != nil {
		panic(err)
	}

	udpConns := map[string]*udpConn{
		"audio": {port: 4000, payloadType: 111},
		"video": {port: 4002, payloadType: 96},
	}
	for _, c := range udpConns {
		// Create remote addr
		var raddr *net.UDPAddr
		if raddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", c.port)); err != nil {
			panic(err)
		}

		// Dial udp
		if c.conn, err = net.DialUDP("udp", laddr, raddr); err != nil {
			panic(err)
		}
		defer func(conn net.PacketConn) {
			if closeErr := conn.Close(); closeErr != nil {
				panic(closeErr)
			}
		}(c.conn)
	}

	// Retrieve udp connection
	c, ok := udpConns[track.Kind().String()]
	if !ok {
		return
	}

	// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
	go func() {
		ticker := time.NewTicker(time.Second * 2)
		for range ticker.C {
			/*
				// We need to add direct access to the peerconnection to ion-sdk-go to support PLI here
				// PLI is disabled in this example currently

				if rtcpErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}}); rtcpErr != nil {
				fmt.Println(rtcpErr)
			}*/
		}
	}()

	b := make([]byte, 1500)
	rtpPacket := &rtp.Packet{}
	for {
		// Read
		n, _, readErr := track.Read(b)
		if readErr != nil {
			panic(readErr)
		}

		// Unmarshal the packet and update the PayloadType
		if err = rtpPacket.Unmarshal(b[:n]); err != nil {
			panic(err)
		}
		rtpPacket.PayloadType = c.payloadType

		// Marshal into original buffer with updated PayloadType
		if n, err = rtpPacket.MarshalTo(b); err != nil {
			panic(err)
		}

		// Write
		if _, err = c.conn.Write(b[:n]); err != nil {
			// For this particular example, third party applications usually timeout after a short
			// amount of time during which the user doesn't have enough time to provide the answer
			// to the browser.
			// That's why, for this particular example, the user first needs to provide the answer
			// to the browser then open the third party application. Therefore we must not kill
			// the forward on "connection refused" errors
			if opError, ok := err.(*net.OpError); ok && opError.Err.Error() == "write: connection refused" {
				continue
			}
			panic(err)
		}
	}
}

func main() {
	// parse flag
	var session, addr, file string
	flag.StringVar(&file, "file", "./file.webm", "Path to the file media")
	flag.StringVar(&addr, "addr", "localhost:50051", "Ion-sfu grpc addr")
	flag.StringVar(&session, "session", "test session", "join session name")
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
		WebRTC: sdk.WebRTCTransportConfig{
			Configuration: webrtcCfg,
		},
	}
	// new sdk engine
	e := sdk.NewEngine(config)

	// create a new client from engine
	c, err := sdk.NewClient(e, addr, "client id")
	if err != nil {
		log.Errorf("err=%v", err)
		return
	}

	// subscribe rtp from sessoin
	// comment this if you don't need save to file
	c.OnTrack = trackToRTP

	// client join a session
	err = c.Join(session, nil)

	// publish file to session if needed
	if err != nil {
		log.Errorf("err=%v", err)
	}
	select {}
}
