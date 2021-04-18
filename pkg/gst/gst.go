package gst

/*
#cgo pkg-config: gstreamer-1.0 gstreamer-app-1.0

#include "gst.h"

*/
import "C"
import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

func MainLoop() {
	C.gstreamer_start_mainloop()
}

// Pipeline is a wrapper for a GStreamer Pipeline
type Pipeline struct {
	Pipeline *C.GstElement

	inAudioTrack *webrtc.TrackRemote
	inVideoTrack *webrtc.TrackRemote
}

var boundTracks = make(map[string]*webrtc.TrackLocalStaticSample)
var boundRemoteTrackKeyframeCallbacks = make(map[string]func())

var pipelinesLock sync.Mutex

func getEncoderString() string {
	if runtime.GOOS == "darwin" {
		return "vtenc_h264 realtime=true allow-frame-reordering=false max-keyframe-interval=60 ! video/x-h264, profile=baseline ! h264parse config-interval=1"
	}
	return "x264enc bframes=0 speed-preset=ultrafast key-int-max=60 ! video/x-h264, profile=baseline ! h264parse config-interval=1"
}

func getDecoderString() string {
	// if runtime.GOOS == "darwin" {
	// return "vtdec"
	// }
	return "avdec_h264"
}

func CreatePipeline(pipelineStr string) *Pipeline {
	log.Debugf("Creating gst pipeline:\n%s", pipelineStr)
	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	pipelinesLock.Lock()
	defer pipelinesLock.Unlock()
	pipeline := &Pipeline{
		Pipeline: C.gstreamer_create_pipeline(pipelineStrUnsafe),
	}
	return pipeline
}

// CreateFilePlayerPipeline creates an appsink GStreamer Pipeline to send a local video file
func CreateFilePlayerPipeline(containerPath string, audioTrack, videoTrack *webrtc.TrackLocalStaticSample) *Pipeline {
	pipelineStr := fmt.Sprintf(`
		filesrc location="%s" !
		decodebin name=demux !
			queue !
			%s ! 
			video/x-h264,stream-format=byte-stream,profile=baseline !
			appsink name=%s
		demux. ! 
			queue ! 
			audioconvert ! 
			audioresample ! 
			audio/x-raw,rate=48000,channels=2 !
			opusenc ! appsink name=%s
	`, containerPath, getEncoderString(), videoTrack.ID(), audioTrack.ID())
	log.Debugf("Creating gst player pipeline:\n%s", pipelineStr)

	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	pipelinesLock.Lock()
	defer pipelinesLock.Unlock()
	pipeline := &Pipeline{
		Pipeline: C.gstreamer_create_pipeline(pipelineStrUnsafe),
	}
	return pipeline
}

// CreateLocalViewerPipeline creates an appsrc GStreamer Pipeline that decodes and shows video in a window locally
func CreateLocalViewerPipeline(audioTrack *webrtc.TrackRemote, videoTrack *webrtc.TrackRemote) *Pipeline {
	pipelineStr := ""
	if audioTrack != nil {
		audioPipe := fmt.Sprintf("appsrc format=time is-live=true do-timestamp=true name=%s ! application/x-rtp", audioTrack.ID())
		switch strings.ToLower(audioTrack.Codec().MimeType) {
		case "audio/opus":
			audioPipe += ", encoding-name=OPUS, payload=96 ! rtpopusdepay ! decodebin ! autoaudiosink"
		case "audio/g722":
			audioPipe += " clock-rate=8000 ! rtpg722depay ! decodebin ! autoaudiosink"
		default:
			panic(fmt.Sprintf("couldn't build gst pipeline for codec: %s ", audioTrack.Codec().MimeType))
		}
		pipelineStr += audioPipe + "\n"
	}

	if videoTrack != nil {
		videoPipe := fmt.Sprintf("appsrc format=time is-live=true name=%s ! application/x-rtp, payload=%d", videoTrack.ID(), videoTrack.Codec().PayloadType)
		switch strings.ToLower(videoTrack.Codec().MimeType) {
		case "video/vp8":
			videoPipe += ", encoding-name=VP8-DRAFT-IETF-01 ! rtpvp8depay ! avdec_vp8 ! autovideosink"
		case "viode/vp9":
			videoPipe += " ! rtpvp9depay ! decodebin ! autovideosink"
		case "video/h264":
			videoPipe += fmt.Sprintf(" ! rtph264depay ! h264parse config-interval=-1 ! %s ! glimagesink sync=false", getDecoderString())
		default:
			panic(fmt.Sprintf("couldn't build gst pipeline for codec: %s ", videoTrack.Codec().MimeType))
		}
		pipelineStr += videoPipe + "\n"
	}
	log.Infof("Creating gst client pipeline:\n%s", pipelineStr)

	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	pipelinesLock.Lock()
	defer pipelinesLock.Unlock()
	pipeline := &Pipeline{
		Pipeline:     C.gstreamer_create_pipeline(pipelineStrUnsafe),
		inAudioTrack: audioTrack,
		inVideoTrack: videoTrack,
	}
	return pipeline
}

// CreateTestSrcPipeline creates a appsink GStreamer Pipeline with test sources to play a test feed into a session
func CreateTestSrcPipeline(audioTrack, videoTrack *webrtc.TrackLocalStaticSample) *Pipeline {
	pipelineStr := fmt.Sprintf(`
		videotestsrc ! 
			video/x-raw,width=1280,height=720 !
			queue !
			%s ! 
			video/x-h264,stream-format=byte-stream,profile=baseline !
			appsink name=%s 
		audiotestsrc wave=6 ! 
			queue ! 
			audioconvert ! 
			audioresample ! 
			audio/x-raw,rate=48000,channels=2 !
			opusenc ! appsink name=%s
	`, getEncoderString(), videoTrack.ID(), audioTrack.ID())

	log.Debugf("Creating gst test pipeline:\n%s", pipelineStr)
	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	pipelinesLock.Lock()
	defer pipelinesLock.Unlock()
	pipeline := &Pipeline{
		Pipeline: C.gstreamer_create_pipeline(pipelineStrUnsafe),
	}
	return pipeline
}

func (p *Pipeline) BindAppsinkToTrack(t *webrtc.TrackLocalStaticSample) {
	log.Debugf("binding appsink to track: %v", t.ID())
	trackIdUnsafe := C.CString(t.ID())
	boundTracks[t.ID()] = t
	C.gstreamer_send_bind_appsink_track(p.Pipeline, trackIdUnsafe, trackIdUnsafe)
}

// Start starts the GStreamer Pipeline
func (p *Pipeline) Start() {
	// This will signal to goHandlePipelineBuffer
	// and provide a method for cancelling sends.
	C.gstreamer_start_pipeline(p.Pipeline)
}

// Play sets the pipeline to PLAYING
func (p *Pipeline) Play() {
	C.gstreamer_play_pipeline(p.Pipeline)
}

// Pause sets the pipeline to PAUSED
func (p *Pipeline) Pause() {
	C.gstreamer_pause_pipeline(p.Pipeline)
}

// SeekToTime seeks on the pipeline
func (p *Pipeline) SeekToTime(seekPos int64) {
	C.gstreamer_seek(p.Pipeline, C.int64_t(seekPos))
}

const (
	videoClockRate = 90000
	audioClockRate = 48000
)

// Push pushes a buffer on the appsrc of the GStreamer Pipeline
func (p *Pipeline) Push(buffer []byte, appsrc string) {
	b := C.CBytes(buffer)
	defer C.free(b)
	inputElementUnsafe := C.CString(appsrc)
	// defer C.free(unsafe.Pointer(&inputElementUnsafe))
	C.gstreamer_receive_push_buffer(p.Pipeline, b, C.int(len(buffer)), inputElementUnsafe)
}

//export goHandlePipelineBuffer
func goHandlePipelineBuffer(buffer unsafe.Pointer, bufferLen C.int, duration C.int, localTrackID *C.char) {
	// log.Debugf("localtrack: %v", C.GoString(localTrackID))
	var track *webrtc.TrackLocalStaticSample = boundTracks[C.GoString(localTrackID)]
	goDuration := time.Duration(duration)
	if track == nil {
		log.Errorf("nil track: %v ", C.GoString(localTrackID))
		return
	}
	if err := track.WriteSample(media.Sample{Data: C.GoBytes(buffer, bufferLen), Duration: goDuration}); err != nil && err != io.ErrClosedPipe {
		panic(err)
	}

	C.free(buffer)
}
