package main

import (
	"flag"
	"fmt"
	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	gst "github.com/pion/ion-sdk-go/example/track-to-gst/gst-receiver"
	"github.com/pion/webrtc/v3"
)

var stream_key = ""
var rtmp_url = "rtmp://x.rtmp.youtube.com/live2" + "/"+stream_key

var streams = map[string]gst.RemoteStream{}

func mapTracks(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	stream, exists := streams[track.StreamID()]
	if !exists {

		stream = gst.RemoteStream{ID: track.StreamID()}
		streams[stream.ID] = stream
		log.Infof("NEW STREAM")
	}

	if track.Kind() == webrtc.RTPCodecTypeAudio {
		stream.AudioTrack = track
		streams[stream.ID] = stream
		log.Infof("GOT AUDIO")
	} else if track.Kind() == webrtc.RTPCodecTypeVideo {
		stream.VideoTrack = track
		streams[stream.ID] = stream
		log.Infof("GOT VIDEO")
	}

	if stream.AudioTrack != nil && stream.VideoTrack != nil {

		videoPipeline := "! videoconvert ! x264enc bitrate=1000 tune=zerolatency ! video/x-h264 ! h264parse ! video/x-h264 ! queue ! flvmux name=mux"
		audioPipeline := "! audioconvert ! audioresample ! audio/x-raw,rate=48000 ! voaacenc bitrate=96000 ! audio/mpeg ! aacparse ! audio/mpeg, mpegversion=4"

		pipelineCommand := fmt.Sprintf(`
			appsrc format=time is-live=true do-timestamp=true name=video-src
				%s
				! rtmpsink location='%s'
			appsrc format=time is-live=true do-timestamp=true name=audio-src
				%s
				! mux.
		`, videoPipeline, rtmp_url, audioPipeline)

		log.Infof("LAUNCHING PIPELINE: %s", pipelineCommand)

		// From https://stackoverflow.com/questions/41550901/how-to-stream-via-rtmp-using-gstreamer
		pipeline := gst.CreateStreamPipeline(stream, pipelineCommand)

		buf := make([]byte, 1400)
		for {
			i, _, readErr := track.Read(buf)
			if readErr != nil {
				panic(readErr)
			}

			pipeline.Push(buf[:i])
		}
	}
}

func main() {
	// init log
	fixByFile := []string{"asm_amd64.s", "proc.go", "icegatherer.go"}
	fixByFunc := []string{"AddProducer", "NewClient"}
	log.Init("debug", fixByFile, fixByFunc)

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
		Log: log.Config{
			Level: "debug",
		},
		WebRTC: sdk.WebRTCTransportConfig{
			Configuration: webrtcCfg,
		},
	}
	// new sdk engine
	e := sdk.NewEngine(config)

	// get a client from engine
	c := e.AddClient(addr, session, "client id")

	// subscribe rtp from sessoin
	// comment this if you don't need save to file
	c.OnTrack = mapTracks

	// client join a session
	err := c.Join(session)

	// publish file to session if needed
	if err != nil {
		log.Errorf("err=%v", err)
	}
	select {}
}
