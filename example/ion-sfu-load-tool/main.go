package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lucsky/cuid"
	ilog "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

var (
	log = ilog.NewLoggerWithFields(ilog.DebugLevel, "main", nil)
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
			c, err := e.NewClient(sdk.ClientConfig{
				Addr: addr,
				Sid:  session,
				Uid:  cid,
			})

			if err != nil {
				log.Errorf("error: %v", err)
				break
			}

			err = c.Join(session)
			if err != nil {
				log.Errorf("error: %v", err)
				break
			}
			err = c.PublishFile(file, video, audio)
			if err != nil {
				log.Errorf("error: %v", err)
				os.Exit(-1)
			}

		case "sub":
			cid := fmt.Sprintf("%s_sub_%d_%s", session, i, cuid.New())
			log.Infof("AddClient session=%v clientid=%v", session, cid)
			c, err := e.NewClient(sdk.ClientConfig{
				Addr: addr,
				Sid:  session,
				Uid:  cid,
			})

			if err != nil {
				log.Errorf("%v", err)
				break
			}

			c.OnTrackEvent = func(event sdk.TrackEvent) {
				log.Infof("OnTrackEvent===: %+v", event)
				var infos []*sdk.Subscription
				for _, t := range event.Tracks {
					// sub audio or not
					if audio && t.Kind == "audio" {
						infos = append(infos, &sdk.Subscription{
							TrackId:   t.Id,
							Mute:      t.Muted,
							Subscribe: true,
							Layer:     t.Layer,
						})
						continue
					}
					// sub one layer
					if simulcast != "" && t.Kind == "video" && t.Layer == simulcast {
						infos = append(infos, &sdk.Subscription{
							TrackId:   t.Id,
							Mute:      t.Muted,
							Subscribe: true,
							Layer:     t.Layer,
						})
						continue
					}
					// sub all if not set simulcast
					if t.Kind == "video" && simulcast == "" {
						infos = append(infos, &sdk.Subscription{
							TrackId:   t.Id,
							Mute:      t.Muted,
							Subscribe: true,
							Layer:     t.Layer,
						})
					}
				}
				err = c.Subscribe(infos)
				if err != nil {
					log.Errorf("error: %v", err)
				}
			}

			err = c.Join(session)
			if err != nil {
				log.Errorf("error: %v", err)
				break
			}

		case "pub":
			cid := fmt.Sprintf("%s_pub_%d_%s", session, i, cuid.New())
			log.Infof("AddClient session=%v clientid=%v", session, cid)
			c, err := e.NewClient(sdk.ClientConfig{
				Addr: addr,
				Sid:  session,
				Uid:  cid,
			})
			if err != nil {
				log.Errorf("%v", err)
				break
			}
			config := sdk.NewJoinConfig().SetNoAutoSubscribe()
			err = c.Join(session, *config)
			if err != nil {
				log.Errorf("error: %v", err)
				break
			}
			err = c.PublishFile(file, video, audio)
			if err != nil {
				log.Errorf("error: %v", err)
				os.Exit(-1)
			}

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
	flag.StringVar(&gaddr, "gaddr", "localhost:5551", "ion-sfu grpc addr")
	flag.StringVar(&session, "session", "ion", "join session name")
	flag.IntVar(&total, "clients", 1, "Number of clients to start")
	flag.IntVar(&cycle, "cycle", 1000, "Run new client cycle in ms")
	flag.IntVar(&duration, "duration", 3600, "Running duration in sencond")
	flag.StringVar(&role, "role", "pubsub", "Run as pubsub/sub/pub")
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

	sdk.DefaultConfig.WebRTC = sdk.WebRTCTransportConfig{
		VideoMime:     "video/vp8",
		Setting:       se,
		Configuration: webrtcCfg,
	}
	sdk.DefaultConfig.LogLevel = loglevel
	if gaddr == "" {
		log.Errorf("gaddr is \"\"!")
		return
	}

	e := sdk.NewEngine()
	if paddr != "" {
		go e.ServePProf(paddr)
	}
	run(e, gaddr, session, file, role, total, duration, cycle, video, audio, simulcast)
}
