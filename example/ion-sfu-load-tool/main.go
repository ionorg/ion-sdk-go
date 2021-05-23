package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/lucsky/cuid"
	ilog "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

var (
	log = ilog.NewLoggerWithFields(ilog.DebugLevel, "", nil)
)

func run(e *sdk.Engine, addr, session, file, role string, total, duration, cycle int, video, audio bool, simulcast string) {
	log.Infof("run session=%v file=%v role=%v total=%v duration=%v cycle=%v video=%v audio=%v simulcast=%v\n", session, file, role, total, duration, cycle, audio, video, simulcast)
	timer := time.NewTimer(time.Duration(duration) * time.Second)

	go e.Stats(3)
	for i := 0; i < total; i++ {
		switch role {
		case "pubsub":
			cid := fmt.Sprintf("%s_pubsub_%d_%s", session, i, cuid.New())
			log.Infof("AddClient session=%v clientid=%v", session, cid)
			c, err := sdk.NewClient(e, addr, cid)
			if err != nil {
				log.Errorf("%v", err)
				break
			}
			config := sdk.NewJoinConfig()
			c.Join(session, config)
			c.PublishWebm(file, video, audio)
			c.Simulcast(simulcast)
		case "sub":
			cid := fmt.Sprintf("%s_sub_%d_%s", session, i, cuid.New())
			log.Infof("AddClient session=%v clientid=%v", session, cid)
			c, err := sdk.NewClient(e, addr, cid)
			if err != nil {
				log.Errorf("%v", err)
				break
			}
			config := sdk.NewJoinConfig().SetNoPublish()
			c.Join(session, config)
			c.Simulcast(simulcast)
		case "pub":
			cid := fmt.Sprintf("%s_pub_%d_%s", session, i, cuid.New())
			log.Infof("AddClient session=%v clientid=%v", session, cid)
			c, err := sdk.NewClient(e, addr, cid)
			if err != nil {
				log.Errorf("%v", err)
				break
			}
			config := sdk.NewJoinConfig().SetNoSubscribe()
			c.Join(session, config)
			c.Simulcast(simulcast)
		default:
			log.Errorf("invalid role! should be pubsub/sub/pub")
		}

		time.Sleep(time.Millisecond * time.Duration(cycle))
	}

	select {
	case <-timer.C:
	}
}

func main() {
	//get args
	var session, gaddr, file, role, loglevel, simulcast, paddr string
	var total, cycle, duration int
	var video, audio bool

	flag.StringVar(&file, "file", "./file.webm", "Path to the file media")
	flag.StringVar(&gaddr, "gaddr", "", "Ion-sfu grpc addr")
	flag.StringVar(&session, "session", "test", "join session name")
	flag.IntVar(&total, "clients", 1, "Number of clients to start")
	flag.IntVar(&cycle, "cycle", 1000, "Run new client cycle in ms")
	flag.IntVar(&duration, "duration", 3600, "Running duration in sencond")
	flag.StringVar(&role, "role", "pubsub", "Run as pubsub/sub")
	flag.StringVar(&loglevel, "log", "info", "Log level")
	flag.BoolVar(&video, "v", false, "Publish video stream from webm file")
	flag.BoolVar(&audio, "a", false, "Publish audio stream from webm file")
	flag.StringVar(&simulcast, "simulcast", "", "simulcast layer q|h|f")
	flag.StringVar(&paddr, "paddr", "", "pprof listening addr")
	flag.Parse()
	switch loglevel {
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	default:
		log.SetLevel(logrus.DebugLevel)
	}

	se := webrtc.SettingEngine{}
	se.SetEphemeralUDPPortRange(10000, 15000)
	webrtcCfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			webrtc.ICEServer{
				URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
			},
		},
	}
	config := sdk.Config{
		WebRTC: sdk.WebRTCTransportConfig{
			VideoMime:     "video/vp8",
			Setting:       se,
			Configuration: webrtcCfg,
		},
	}
	if gaddr == "" {
		log.Errorf("gaddr is \"\"!")
		return
	}
	e := sdk.NewEngine(config)
	if paddr != "" {
		go e.ServePProf(paddr)
	}
	run(e, gaddr, session, file, role, total, duration, cycle, video, audio, simulcast)
}
