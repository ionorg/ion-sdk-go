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

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

// CompositorPipeline will decode incoming tracks in a single pipeline and compose the streams
type CompositorPipeline struct {
	mu       sync.Mutex
	Pipeline *C.GstElement

	trackBins              map[string]*C.GstElement
	trackKeyframeCallbacks map[string]func()
}

// NewCompositorPipeline will create a pipeline controller for AV compositing.  It will include tee's (vtee,atee) for linking extra elements to the composited output using the extraPipelineStr
func NewCompositorPipeline(extraPipelineStr string) *CompositorPipeline {
	pipelineStr := `
		compositor name=vmix background=black ! video/x-raw,width=1920,height=1080,framerate=30/1,format=UYVY ! queue ! tee name=vtee 
			vtee. ! queue ! glimagesink sync=false 
		audiomixer name=amix ! queue ! tee name=atee 
			atee. ! queue ! audioconvert ! autoaudiosink
	` + extraPipelineStr
	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	c := &CompositorPipeline{
		Pipeline:  C.gstreamer_create_pipeline(pipelineStrUnsafe),
		trackBins: make(map[string]*C.GstElement),
	}
	runtime.SetFinalizer(c, func(c *CompositorPipeline) {
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
		inputBin += ", encoding-name=VP8-DRAFT-IETF-01 ! rtpvp8depay ! decodebin "
	case "viode/vp9":
		inputBin += " ! rtpvp9depay ! decodebin "
	case "video/h264":
		inputBin += fmt.Sprintf(", payload=%d ! rtph264depay ! queue ! %s !  videoconvert ! queue ", t.PayloadType(), getDecoderString())
	default:
		panic(fmt.Sprintf("couldn't build gst pipeline for codec: %s ", t.Codec().MimeType))
	}

	log.Info("adding input track", "bin", inputBin)
	inputBinUnsafe := C.CString(inputBin)
	// defer C.free(unsafe.Pointer(&inputBinUnsafe))
	trackIdUnsafe := C.CString(t.ID())
	// defer C.free(unsafe.Pointer(&trackIdUnsafe))

	isVideo := t.Kind() == webrtc.RTPCodecTypeVideo
	bin := C.gstreamer_compositor_add_input_track(c.Pipeline, inputBinUnsafe, trackIdUnsafe, C.bool(isVideo))
	c.trackBins[t.ID()] = bin

	if t.Kind() == webrtc.RTPCodecTypeVideo {
		boundRemoteTrackKeyframeCallbacks[t.ID()] = func() {
			log.Info("sending pli for track", "id", t.ID())
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
			log.Info("end of track, cleaning up pipeline", "id", t.ID())

			trackBin := c.trackBins[t.ID()]
			C.gstreamer_compositor_remove_input_track(c.Pipeline, trackBin, C.bool(t.Kind() == webrtc.RTPCodecTypeVideo))
			delete(c.trackBins, t.ID())
			delete(boundRemoteTrackKeyframeCallbacks, t.ID())
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
	log.Info("go forceKeyUnit for track", "id", id)
	if trackSendPLI, ok := boundRemoteTrackKeyframeCallbacks[id]; ok {
		trackSendPLI()
	}
}
