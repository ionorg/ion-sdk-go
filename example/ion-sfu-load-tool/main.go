package main

import (
	"flag"
	"fmt"
	"time"

	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
)

func run(e *sdk.Engine, addr, room, file, role string, total, duration, cycle int) {
	log.Infof("run room=%v file=%v role=%v total=%v duration=%v cycle=%v\n", room, file, role, total, duration, cycle)
	timer := time.NewTimer(time.Duration(duration) * time.Second)

	// go s.Stats(3)
	for i := 0; i < total; i++ {
		switch role {
		case "pubsub":
			log.Infof("room= %v", room)
			c := e.AddClient(addr, room, fmt.Sprintf("%s_%d", room, i))
			if c == nil {
				log.Errorf("c==nil")
				break
			}
			c.AddProducer(file)
		case "pub":
			log.Infof("room= %v", room)
			c := e.AddClient(addr, room, fmt.Sprintf("%s_%d", room, i))
			if c == nil {
				log.Errorf("c==nil")
				break
			}
		case "sub":
			log.Infof("room= %v", room)
			c := e.AddClient(addr, room, fmt.Sprintf("%s_%d", room, i))
			if c == nil {
				log.Errorf("c==nil")
				break
			}
		default:
			log.Errorf("invalid role! should be pub/sub/pubsub")
		}

		time.Sleep(time.Millisecond * time.Duration(cycle))
	}

	select {
	case <-timer.C:
	}
}

func main() {
	//init log
	fixByFile := []string{"asm_amd64.s", "proc.go", "icegatherer.go"}
	fixByFunc := []string{"AddProducer"}

	//get args
	var room string
	var addr, file string
	var total, cycle, duration int
	var role string
	var loglevel string
	// var video, audio bool

	flag.StringVar(&file, "file", "./file.webm", "Path to the file media")
	flag.StringVar(&addr, "addr", "localhost:50051", "Ion-sfu grpc addr")
	flag.StringVar(&room, "room", "room", "Room to join")
	flag.IntVar(&total, "clients", 1, "Number of clients to start")
	flag.IntVar(&cycle, "cycle", 300, "Run new client cycle in ms")
	flag.IntVar(&duration, "duration", 3600, "Running duration in sencond")
	flag.StringVar(&role, "role", "pubsub", "Run as pub/sub/pubsub  (sender/receiver/both)")
	flag.StringVar(&loglevel, "log", "info", "Log level")
	// flag.BoolVar(&video, "video", true, "Publish video stream from webm file")
	// flag.BoolVar(&audio, "audio", true, "Publish audio stream from webm file")
	flag.Parse()
	log.Init(loglevel, fixByFile, fixByFunc)

	se := webrtc.SettingEngine{}
	se.SetEphemeralUDPPortRange(10000, 11000)
	webrtcCfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			webrtc.ICEServer{
				URLs: []string{"stun:stun.stunprotocol.org:3478"},
			},
		},
	}
	config := sdk.Config{
		Log: log.Config{
			Level: loglevel,
		},
		WebRTC: sdk.WebRTCTransportConfig{
			Setting:       se,
			Configuration: webrtcCfg,
		},
	}
	e := sdk.NewEngine(addr, config)
	run(e, addr, room, file, role, total, duration, cycle)
}
