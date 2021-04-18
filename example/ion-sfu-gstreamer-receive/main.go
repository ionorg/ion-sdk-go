package main

import (
	"flag"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	gst "github.com/pion/ion-sdk-go/pkg/gst"
	"github.com/pion/webrtc/v3"
)

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func runClientLoop(addr, session string) {

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
			Level: "debug",
		},
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

	filename := strings.ReplaceAll(session, " ", "-") + "-" + strconv.FormatInt(time.Now().Unix(), 10) + ".avi"
	destination := "filesink location=./" + filename
	videoEncoder := "x264enc bframes=0 speed-preset=ultrafast key-int-max=60 ! video/x-h264, profile=baseline "
	//destination="movie.avi"

	compositorString := fmt.Sprintf(`
		qtmux name=savemux ! queue ! %s sync=false async=false
			vtee. ! queue ! savemux.
			atee. ! queue ! savemux.

	`, destination)

	log.Infof("Beginning Recording Compositor[%s]: %s -> %s", addr, videoEncoder, filename)

	pipelineID := addr + "|" + filename
	log.Infof("connected pipeline[%s]!", pipelineID)
	compositor := gst.NewCompositorPipeline(compositorString)

	c.OnTrack = func(t *webrtc.TrackRemote, r *webrtc.RTPReceiver) {
		log.Debugf("pipeline[%s] got track: %#v", pipelineID, t)
		if t.Kind() == webrtc.RTPCodecTypeVideo && t.Codec().MimeType != webrtc.MimeTypeH264 {
			log.Errorf("only h264 video is supported currently, please help me improve this example :) ")
			panic("exiting")
		}

		compositor.AddInputTrack(t, c.GetSubTransport().GetPeerConnection())
	}

	compositor.OnRemoveTrack = func(t *webrtc.TrackRemote) {
		log.Infof("REMOVED TRACK", t.Codec(), len(compositor.Tracks))
	}

	// client join a session
	err = c.Join(session)
	if err != nil {
		log.Errorf("error joining room:", err)
		panic(err)
	}

	log.Infof("joined pipeline[%s]!", pipelineID)
	compositor.Play()
	log.Infof("compositing!")

}

func main() {
	// init log
	fixByFile := []string{"asm_amd64.s", "proc.go", "icegatherer.go"}
	fixByFunc := []string{"AddProducer", "NewClient"}
	log.Init("debug", fixByFile, fixByFunc)

	// parse flag
	var session, addr string
	flag.StringVar(&addr, "addr", "localhost:50051", "ion-cluster grpc addr")
	flag.StringVar(&session, "session", "test room", "join session name")
	flag.Parse()

	go runClientLoop(addr, session)

	gst.MainLoop()
}
