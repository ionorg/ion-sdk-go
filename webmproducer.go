package engine

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/ebml-go/webm"
	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

type trackInfo struct {
	track *webrtc.TrackLocalStaticSample
	rate  int
}

// WebMProducer support streaming by webm which encode with vp8 and opus
type WebMProducer struct {
	name          string
	stop          bool
	paused        bool
	pauseChan     chan bool
	seekChan      chan time.Duration
	offsetSeconds int
	reader        *webm.Reader
	webm          webm.WebM
	trackMap      map[uint]*trackInfo
	file          *os.File
	sendByte      int
}

// NewWebMProducer new a WebMProducer
func NewWebMProducer(name string, offset int) *WebMProducer {
	r, err := os.Open(name)
	if err != nil {
		log.Errorf("unable to open file %s", name)
		return nil
	}
	var w webm.WebM
	reader, err := webm.Parse(r, &w)
	if err != nil {
		log.Errorf("error: %v", err)
		return nil
	}

	p := &WebMProducer{
		name:          name,
		offsetSeconds: offset,
		reader:        reader,
		webm:          w,
		trackMap:      make(map[uint]*trackInfo),
		file:          r,
		pauseChan:     make(chan bool),
		seekChan:      make(chan time.Duration, 1),
	}

	return p
}

func (t *WebMProducer) Stop() {
	t.stop = true
	t.reader.Shutdown()
}

func (t *WebMProducer) Start() {
	go t.readLoop()
}

func (t *WebMProducer) SeekP(ts int) {
	seekDuration := time.Duration(ts) * time.Second
	t.seekChan <- seekDuration
}

func (t *WebMProducer) Pause(pause bool) {
	t.pauseChan <- pause
}

// GetVideoTrack get video track
func (t *WebMProducer) GetVideoTrack() (*webrtc.TrackLocalStaticSample, error) {
	var err error
	var track *webrtc.TrackLocalStaticSample
	streamId := fmt.Sprintf("webm_%p", t)
	vTrack := t.webm.FindFirstVideoTrack()
	if vTrack == nil {
		return nil, errors.New("not video track")
	}
	switch vTrack.CodecID {
	case "V_VP8":
		track, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "video", streamId)
	case "V_VP9":
		track, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9, ClockRate: 90000}, "video", streamId)
	default:
		log.Errorf("Unsupported video codec %v", vTrack.CodecID)
		return nil, err
	}
	t.trackMap[vTrack.TrackNumber] = &trackInfo{track: track, rate: 90000}
	return track, err
}

// GetAudioTrack get audio track
func (t *WebMProducer) GetAudioTrack() (*webrtc.TrackLocalStaticSample, error) {
	streamId := fmt.Sprintf("webm_%p", t)
	aTrack := t.webm.FindFirstAudioTrack()
	if aTrack == nil {
		return nil, errors.New("not audio track")
	}
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", streamId)
	t.trackMap[aTrack.TrackNumber] = &trackInfo{
		track: track,
		rate:  int(aTrack.Audio.OutputSamplingFrequency),
	}
	return track, err
}

func (t *WebMProducer) readLoop() {
	startTime := time.Now()
	timeEps := 5 * time.Millisecond

	seekDuration := time.Duration(-1)

	if t.offsetSeconds > 0 {
		t.SeekP(t.offsetSeconds)
	}

	startSeek := func(seekTime time.Duration) {
		t.reader.Seek(seekTime)
		seekDuration = seekTime
	}

	for pck := range t.reader.Chan {
		if t.paused {
			log.Infof("Paused")
			// Wait for unpause
			for pause := range t.pauseChan {
				if !pause {
					t.paused = false
					break
				}
			}
			log.Infof("Unpaused")
			startTime = time.Now().Add(-pck.Timecode)
		}

		// Restart when track runs out
		if pck.Timecode < 0 {
			if !t.stop {
				log.Infof("Restart media")
				startSeek(0)
			}
			continue
		}

		// Handle seek and pause
		select {
		case dur := <-t.seekChan:
			log.Infof("Seek duration=%v", dur)
			startSeek(dur)
			continue
		case pause := <-t.pauseChan:
			t.paused = pause
			if pause {
				continue
			}
		default:
		}

		// Handle actual seek
		if seekDuration > -1 && math.Abs(float64((pck.Timecode-seekDuration).Milliseconds())) < 30.0 {
			startTime = time.Now().Add(-seekDuration)
			seekDuration = time.Duration(-1)
			continue
		}

		// Find sender
		if track, ok := t.trackMap[pck.TrackNumber]; ok {
			// Only delay frames we care about
			timeDiff := pck.Timecode - time.Since(startTime)
			if timeDiff > timeEps {
				time.Sleep(timeDiff - time.Millisecond)
			}

			// Send samples
			if ivfErr := track.track.WriteSample(media.Sample{Data: pck.Data, Duration: time.Millisecond * 20}); ivfErr != nil {
				log.Errorf("Track write error=%v", ivfErr)
			} else {
				log.Tracef("t=%v mime=%v kind=%v streamid=%v len=%v", t, track.track.Codec().MimeType, track.track.Kind(), track.track.StreamID(), len(pck.Data))
				t.sendByte += len(pck.Data)
			}
		}
	}
	log.Infof("Exiting webm producer")
}

// GetSendBandwidth calc the sending bandwidth with cycle(s)
func (t *WebMProducer) GetSendBandwidth(cycle int) int {
	bw := t.sendByte / cycle / 1000
	t.sendByte = 0
	return bw
}

func validateVPFile(name string) (string, bool) {
	list := strings.Split(name, ".")
	if len(list) < 2 {
		return "", false
	}
	ext := strings.ToLower(list[len(list)-1])
	var valid bool
	// Validate is ivf|webm
	for _, a := range []string{"ivf", "webm"} {
		if a == ext {
			valid = true
		}
	}

	return ext, valid
}
