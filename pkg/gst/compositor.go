package gst

/*
#cgo pkg-config: gstreamer-1.0 gstreamer-app-1.0 gstreamer-video-1.0

#include "gst.h"

*/
import "C"
import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	log "github.com/pion/ion-log"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

// CompositorPipeline will decode incoming tracks in a single pipeline and compose the streams
type CompositorPipeline struct {
	mu       sync.Mutex
	Pipeline *C.GstElement
	OnRemoveTrack func(track *webrtc.TrackRemote)
	Tracks              map[string]*webrtc.TrackRemote

	trackBins              map[string]*C.GstElement
	trackKeyframeCallbacks map[string]func()
}

// NewCompositorPipeline will create a pipeline controller for AV compositing.
// It will include tee's (vtee,atee) for linking extra elements to the composited output using the extraPipelineStr
// You can add more than one audio/video tracks to a pipeline, but having zero tracks will end the pipeline
func NewCompositorPipeline(extraPipelineStr string) *CompositorPipeline {
	pipelineStr := `
		compositor name=vmix background=black ! video/x-raw,width=1920,height=1080,framerate=30/1,format=UYVY ! queue ! tee name=vtee 
			vtee. ! queue ! glimagesink sync=false 
		audiomixer name=amix ! queue ! tee name=atee 
			atee. ! queue ! audioconvert ! autoaudiosink
	` + extraPipelineStr

	log.Infof("Creating gst compositor pipeline:\n%s", pipelineStr)
	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	c := &CompositorPipeline{
		Pipeline:  C.gstreamer_create_pipeline(pipelineStrUnsafe),
		Tracks: make(map[string]*webrtc.TrackRemote),
		trackBins: make(map[string]*C.GstElement),
	}
	runtime.SetFinalizer(c, func(c *CompositorPipeline) {
		log.Infof("Destroying compositor pipeline...")
		c.destroy()
	})
	C.gstreamer_start_pipeline(c.Pipeline)
	return c
}

func (c *CompositorPipeline) AddInputTrack(t *webrtc.TrackRemote, pc *webrtc.PeerConnection) {
	c.mu.Lock()
	defer c.mu.Unlock()

	inputBin := fmt.Sprintf("appsrc format=time is-live=true do-timestamp=true name=%s ! application/x-rtp ", t.ID())

	switch strings.ToLower(t.Codec().MimeType) {
	case "audio/opus":
		inputBin += ", encoding-name=OPUS, payload=96 ! rtpopusdepay ! queue ! opusdec "
	case "audio/g722":
		inputBin += " clock-rate=8000 ! rtpg722depay ! decodebin "
	case "video/vp8":
		inputBin += ", encoding-name=VP8-DRAFT-IETF-01 ! rtpvp8depay ! avdec_vp8 "
	case "viode/vp9":
		inputBin += " ! rtpvp9depay ! decodebin "
	case "video/h264":
		inputBin += fmt.Sprintf(", payload=%d ! rtph264depay ! queue ! %s !  videoconvert ! queue ", t.PayloadType(), getDecoderString())
	default:
		panic(fmt.Sprintf("couldn't build gst pipeline for codec: %s ", t.Codec().MimeType))
	}

	log.Debugf("adding input track with bin: %s", inputBin)
	inputBinUnsafe := C.CString(inputBin)
	// defer C.free(unsafe.Pointer(&inputBinUnsafe))
	trackIdUnsafe := C.CString(t.ID())
	// defer C.free(unsafe.Pointer(&trackIdUnsafe))

	isVideo := t.Kind() == webrtc.RTPCodecTypeVideo
	bin := C.gstreamer_compositor_add_input_track(c.Pipeline, inputBinUnsafe, trackIdUnsafe, C.bool(isVideo))
	c.trackBins[t.ID()] = bin
	c.Tracks[t.ID()] = t

	if t.Kind() == webrtc.RTPCodecTypeVideo {
		boundRemoteTrackKeyframeCallbacks[t.ID()] = func() {
			log.Debugf("sending pli for track %s", t.ID())
			rtcpSendErr := pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(t.SSRC())}})
			if rtcpSendErr != nil {
				fmt.Println(rtcpSendErr)
			}
		}
	}
	go c.bindTrackToAppsrc(t)
}

func (c *CompositorPipeline) Play() {
	c.mu.Lock()
	defer c.mu.Unlock()

	C.gstreamer_play_pipeline(c.Pipeline)
}

func (c *CompositorPipeline) Stop() {
	C.gstreamer_stop_pipeline(c.Pipeline)
}

func (c *CompositorPipeline) destroy() {
	for _, b := range c.trackBins {
		C.gst_object_unref(C.gpointer(b))
	}
}

func (c *CompositorPipeline) bindTrackToAppsrc(t *webrtc.TrackRemote) {
	buf := make([]byte, 1400)
	for {
		i, _, readErr := t.Read(buf)
		if readErr != nil {
			log.Debugf("end of track %v: cleaning up pipeline", t.ID())

			trackBin := c.trackBins[t.ID()]
			C.gstreamer_compositor_remove_input_track(c.Pipeline, trackBin, C.bool(t.Kind() == webrtc.RTPCodecTypeVideo))
			delete(c.trackBins, t.ID())
			delete(c.Tracks, t.ID())
			delete(boundRemoteTrackKeyframeCallbacks, t.ID())

			if c.OnRemoveTrack != nil {
				c.OnRemoveTrack(t)
			}
			// panic(readErr)
			return
		}
		c.pushAppsrc(buf[:i], t.ID())
	}
}

// Push pushes a buffer on the appsrc of the GStreamer Pipeline
func (c *CompositorPipeline) pushAppsrc(buffer []byte, appsrc string) {
	b := C.CBytes(buffer)
	defer C.free(b)
	inputElementUnsafe := C.CString(appsrc)
	// defer C.free(unsafe.Pointer(&inputElementUnsafe))
	C.gstreamer_receive_push_buffer(c.Pipeline, b, C.int(len(buffer)), inputElementUnsafe)
}

//export goHandleAppsrcForceKeyUnit
func goHandleAppsrcForceKeyUnit(remoteTrackID *C.char) {
	id := C.GoString(remoteTrackID)
	log.Debugf("go forceKeyUnit: %v", id)
	if trackSendPLI, ok := boundRemoteTrackKeyframeCallbacks[id]; ok {
		trackSendPLI()
	}
}
