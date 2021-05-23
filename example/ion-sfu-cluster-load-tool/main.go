package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"client"

	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
)

func get_host(addr string, new_session string, notify chan string) {
	resp, err := http.Get(addr + "session/" + new_session)
	if err != nil {
		log.Errorf("%v", err)
		time.Sleep(10 * time.Second)
		get_host(addr, new_session, notify)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("%v", err)
		time.Sleep(10 * time.Second)
		get_host(addr, new_session, notify)
	}
	sfu_host := string(body)
	if sfu_host == "NO_HOSTS_RETRY" {
		fmt.Println("waiting for host to get ready!")
		time.Sleep(2 * time.Second)
		get_host(addr, new_session, notify)
	} else if sfu_host == "SERVER_LOAD" {
		fmt.Println("server is underload need to wait before joining call!")
		time.Sleep(2 * time.Second)
		get_host(addr, new_session, notify)
	} else {
		sfu_host = strings.Replace(sfu_host, "700", "5005", -1)
		// sfu_host = strings.Replace(sfu_host, "7003", "50053", -1)
		sfu_host = strings.Replace(sfu_host, "\"", "", -1)
		fmt.Println("sfu host host", sfu_host, "for session", new_session)
		notify <- sfu_host
	}
}

func run(e *sdk.Engine, addr, session, file, role string, total, duration, cycle int, video, audio bool, simulcast string, create_room int) {
	log.Infof("run session=%v file=%v role=%v total=%v duration=%v cycle=%v video=%v audio=%v simulcast=%v\n", session, file, role, total, duration, cycle, audio, video, simulcast)
	timer := time.NewTimer(time.Duration(duration) * time.Second)

	go e.Stats(3)
	for i := 0; i < total; i++ {
		new_session := session
		if create_room != -1 {
			new_session = new_session + fmt.Sprintf("%v", i%create_room)
		}

		notify := make(chan string)
		go get_host(addr, new_session, notify)
		sfu_host := <-notify

		switch role {
		case "pubsub":
			var producer *client.GSTProducer
			cid := fmt.Sprintf("%s_pubsub_%d", new_session, i)
			log.Infof("AddClient session=%v clientid=%v", new_session, cid)
			c, err := sdk.NewClient(e, sfu_host, cid)
			if err != nil {
				log.Errorf("%v", err)
				break
			}
			c.Join(new_session)
			if !strings.Contains(file, ".webm") {
				if file == "test" {
					producer = client.NewGSTProducer(c, "video", "")
				} else {
					producer = client.NewGSTProducer(c, "screen", file)
				}
				log.Infof("publishing tracks")

				c.GetPubTransport().GetPeerConnection().AddTrack(producer.videoTrack())

				log.Infof("tracks published")
			} else {
				c.PublishWebm(file, video, audio)
			}
			c.Simulcast(simulcast)
		case "sub":
			cid := fmt.Sprintf("%s_sub_%d", new_session, i)
			log.Infof("AddClient session=%v clientid=%v", new_session, cid)
			c, err := sdk.NewClient(e, sfu_host, cid)
			if err != nil {
				log.Errorf("%v", err)
				break
			}
			c.Join(new_session)
			c.Simulcast(simulcast)
		default:
			log.Errorf("invalid role! should be pubsub/sub")
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

	gst.MainLoop()

	var create_room = -1

	flag.StringVar(&file, "file", "./big-buck-bunny_trailer.webm", "Path to the file media")
	flag.StringVar(&gaddr, "gaddr", "http://0.0.0.0:4000/", "Ion-sfu grpc addr")
	flag.StringVar(&session, "session", "test", "join session name")
	flag.IntVar(&total, "clients", 1, "Number of clients to start")
	flag.IntVar(&cycle, "cycle", 1000, "Run new client cycle in ms")
	flag.IntVar(&duration, "duration", 3600, "Running duration in sencond")
	flag.StringVar(&role, "role", "pubsub", "Run as pubsub/sub")
	flag.StringVar(&loglevel, "log", "info", "Log level")
	flag.BoolVar(&video, "v", true, "Publish video stream from webm file")
	flag.BoolVar(&audio, "a", true, "Publish audio stream from webm file")
	flag.StringVar(&simulcast, "simulcast", "", "simulcast layer q|h|f")
	flag.StringVar(&paddr, "paddr", "", "pprof listening addr")
	flag.IntVar(&create_room, "create_room", -1, "number of peers per room")
	flag.Parse()
	log.Init(loglevel)

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
		Log: log.Config{
			Level: loglevel,
		},
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
	run(e, gaddr, session, file, role, total, duration, cycle, video, audio, simulcast, create_room)
}
