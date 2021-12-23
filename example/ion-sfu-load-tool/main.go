package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
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

var (
	errInvalidSessID = errors.New("invalid session id")
)

// Engine a sdk engine
type Engine struct {
	sync.RWMutex
	clients   map[string]map[string]*sdk.RTC
	connector *sdk.Connector
	config    *sdk.RTCConfig
}

// NewEngine create a engine
func NewEngine(addr string) *Engine {
	e := &Engine{
		clients:   make(map[string]map[string]*sdk.RTC),
		connector: sdk.NewConnector(addr),
	}

	se := webrtc.SettingEngine{}
	se.SetEphemeralUDPPortRange(10000, 15000)
	e.config = &sdk.RTCConfig{
		WebRTC: sdk.WebRTCTransportConfig{
			VideoMime: "video/vp8",
			Setting:   se,
			Configuration: webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{
						URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
					},
				},
			},
		},
	}

	return e
}

func (e *Engine) AddClient(sid, cid string) *sdk.RTC {
	e.Lock()
	defer e.Unlock()
	if e.clients[sid] == nil {
		e.clients[sid] = make(map[string]*sdk.RTC)
	}

	c := sdk.NewRTC(e.connector)
	e.clients[sid][cid] = c
	c.OnError = func(err error) {
		log.Errorf("engine got error: %v", err)
		e.DelClient(sid, cid)
	}
	return c
}

func (e *Engine) DelClient(sid, cid string) {
	e.Lock()
	defer e.Unlock()
	if e.clients[sid] == nil {
		e.Unlock()
		log.Errorf("error: %v", errInvalidSessID)
		return
	}

	if c, ok := e.clients[sid][cid]; ok && (c != nil) {
		c.Close()
	}

	delete(e.clients[sid], cid)
	if len(e.clients[sid]) == 0 {
		delete(e.clients, sid)
	}
}

// Stats show a total stats to console: clients and bandwidth
func (e *Engine) Stats(cycle int) string {
	for {
		info := "\n-------stats-------\n"

		e.RLock()
		if len(e.clients) == 0 {
			e.RUnlock()
			time.Sleep(time.Second)
			continue
		}
		n := 0
		for _, m := range e.clients {
			n += len(m)
		}
		info += fmt.Sprintf("Clients: %d\n", n)

		totalRecvBW, totalSendBW := 0, 0
		for _, m := range e.clients {
			for _, c := range m {
				if c == nil {
					continue
				}
				recvBW, sendBW := c.GetBandWidth(cycle)
				totalRecvBW += recvBW
				totalSendBW += sendBW
			}
		}

		info += fmt.Sprintf("RecvBandWidth: %d KB/s\n", totalRecvBW)
		info += fmt.Sprintf("SendBandWidth: %d KB/s\n", totalSendBW)
		e.RUnlock()
		log.Infof(info)
		time.Sleep(time.Duration(cycle) * time.Second)
	}
}

// ServePProf listening pprof
func (e *Engine) ServePProf(paddr string) {
	log.Infof("PProf Listening %v", paddr)
	err := http.ListenAndServe(paddr, nil)
	if err != nil {
		log.Errorf("ServePProf error:%v", err)
	}
}

func run(e *Engine, addr, session, file, role string, total, duration, cycle int, video, audio bool, simulcast string) {
	log.Infof("run session=%v file=%v role=%v total=%v duration=%v cycle=%v video=%v audio=%v simulcast=%v\n", session, file, role, total, duration, cycle, audio, video, simulcast)
	timer := time.NewTimer(time.Duration(duration) * time.Second)

	go e.Stats(3)
	for i := 0; i < total; i++ {
		switch role {
		case "pubsub":
			cid := fmt.Sprintf("%s_pubsub_%d_%s", session, i, cuid.New())
			log.Infof("AddClient session=%v clientid=%v", session, cid)
			c := e.AddClient(session, cid)
			err := c.Join(session, cid)
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
			c := e.AddClient(session, cid)

			c.OnTrackEvent = func(event sdk.TrackEvent) {
				_ = c.SubscribeFromEvent(event, audio, video, simulcast)
			}

			config := sdk.NewJoinConfig().SetNoAutoSubscribe()
			err := c.Join(session, cid, config)

			if err != nil {
				log.Errorf("%v", err)
				break
			}

		case "pub":
			cid := fmt.Sprintf("%s_pub_%d_%s", session, i, cuid.New())
			log.Infof("AddClient session=%v clientid=%v", session, cid)
			c := e.AddClient(session, cid)
			err := c.Join(session, cid)
			if err != nil {
				log.Errorf("%v", err)
				break
			}
			config := sdk.NewJoinConfig().SetNoAutoSubscribe()
			err = c.Join(session, cid, config)
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
	flag.BoolVar(&video, "v", true, "Publish video stream from webm file")
	flag.BoolVar(&audio, "a", true, "Publish audio stream from webm file")
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

	e := NewEngine(gaddr)
	if paddr != "" {
		go e.ServePProf(paddr)
	}
	run(e, gaddr, session, file, role, total, duration, cycle, video, audio, simulcast)
}
