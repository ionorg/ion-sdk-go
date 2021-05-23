package client

import (
	"fmt"

	"./gst"
	"github.com/lucsky/cuid"
	"github.com/pion/webrtc/v3"
)

//Producer interface
type Producer interface {
	Start()
	Stop()

	AudioTrack() *webrtc.TrackLocalStaticSample
	VideoTrack() *webrtc.TrackLocalStaticSample
}

// GSTProducer will produce audio + video from a gstreamer pipeline and can be published to a client
type GSTProducer struct {
	name       string
	audioTrack *webrtc.TrackLocalStaticSample
	videoTrack *webrtc.TrackLocalStaticSample
	pipeline   *gst.Pipeline
	paused     bool
}

// NewGSTProducer will create a new producer for a given client and a videoFile
func NewGSTProducer(c *Client, kind string, path string) *GSTProducer {
	stream := fmt.Sprintf("gst-%v-%v", kind, cuid.New())
	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "video/h264", ClockRate: 90000}, cuid.New(), stream)
	if err != nil {
		panic(err)
	}

	audioTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "audio/opus", ClockRate: 48000}, cuid.New(), stream)
	if err != nil {
		panic(err)
	}

	var pipeline *gst.Pipeline
	if path != "" {
		pipeline = gst.CreatePlayerPipeline(path, audioTrack, videoTrack)
	} else {
		pipeline = gst.CreateTestSrcPipeline(audioTrack, videoTrack)
	}

	pipeline.BindAppsinkToTrack(videoTrack)
	pipeline.BindAppsinkToTrack(audioTrack)

	return &GSTProducer{
		videoTrack: videoTrack,
		audioTrack: audioTrack,
		pipeline:   pipeline,
	}
}

//AudioTrack returns the audio track for the pipeline
func (t *GSTProducer) AudioTrack() *webrtc.TrackLocalStaticSample {
	return t.audioTrack
}

//VideoTrack returns the video track for the pipeline
func (t *GSTProducer) VideoTrack() *webrtc.TrackLocalStaticSample {
	return t.videoTrack
}

//SeekP to a timestamp
func (t *GSTProducer) SeekP(ts int) {
	t.pipeline.SeekToTime(int64(ts))
}

//Pause the pipeline
func (t *GSTProducer) Pause(pause bool) {
	if pause {
		t.pipeline.Pause()
	} else {
		t.pipeline.Play()
	}
}

//Stop the pipeline
func (t *GSTProducer) Stop() {
}

//Start the pipeline
func (t *GSTProducer) Start() {
	t.pipeline.Start()
	t.pipeline.Play()
}
