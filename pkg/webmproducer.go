package engine

import (
	"errors"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/ebml-go/webm"
	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	errInvalidPC   = errors.New("invalid pc")
	errInvalidKind = errors.New("invalid kind, shoud be audio or video")
)

type trackInfo struct {
	track         *webrtc.Track
	rate          int
	lastFrameTime time.Duration
}

// WebMProducer support streaming by webm which encode with vp8 and opus
type WebMProducer struct {
	name          string
	stop          bool
	paused        bool
	pauseChan     chan bool
	seekChan      chan time.Duration
	videoTrack    *webrtc.Track
	audioTrack    *webrtc.Track
	offsetSeconds int
	reader        *webm.Reader
	webm          webm.WebM
	trackMap      map[uint]*trackInfo
	videoCodec    string
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
		log.Errorf("err=%v", err)
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

func (t *WebMProducer) AudioTrack() *webrtc.Track {
	return t.audioTrack
}

func (t *WebMProducer) VideoTrack() *webrtc.Track {
	return t.videoTrack
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

func (t *WebMProducer) VideoCodec() string {
	return t.videoCodec
}

// AddTrack will add new track to pc
func (t *WebMProducer) AddTrack(pc *webrtc.PeerConnection, kind string) (*webrtc.Track, error) {
	if pc == nil {
		return nil, errInvalidPC
	}
	if kind != "video" && kind != "audio" {
		return nil, errInvalidKind
	}

	var track *webrtc.Track
	var err error
	if kind == "video" {
		if vTrack := t.webm.FindFirstVideoTrack(); vTrack != nil {
			log.Infof("Video codec %v", vTrack.CodecID)

			var videoCodedID uint8
			switch vTrack.CodecID {
			case "V_VP8":
				videoCodedID = webrtc.DefaultPayloadTypeVP8
				t.videoCodec = webrtc.VP8
			case "V_VP9":
				videoCodedID = webrtc.DefaultPayloadTypeVP9
				t.videoCodec = webrtc.VP9
			default:
				log.Errorf("Unsupported video codec %v", vTrack.CodecID)
			}

			track, err = pc.NewTrack(videoCodedID, rand.Uint32(), "video", "pion")
			if err != nil {
				return nil, err
			}

			pc.AddTrack(track)
			t.trackMap[vTrack.TrackNumber] = &trackInfo{track: track, rate: 90000}
			t.videoTrack = track
			log.Infof("t.trackMap=%+v", t.trackMap)
		}
	} else if kind == "audio" {
		if aTrack := t.webm.FindFirstAudioTrack(); aTrack != nil {
			log.Infof("Audio codec %v", aTrack.CodecID)
			track, err := pc.NewTrack(webrtc.DefaultPayloadTypeOpus, rand.Uint32(), "audio", "pion")
			if err != nil {
				panic(err)
			}

			pc.AddTrack(track)
			t.trackMap[aTrack.TrackNumber] = &trackInfo{
				track: track,
				rate:  int(aTrack.Audio.OutputSamplingFrequency),
			}
			t.audioTrack = track
			log.Infof("t.trackMap=%+v", t.trackMap)
		}
	}

	return track, nil
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
			log.Infof("Seek happened!!!!")
			startTime = time.Now().Add(-seekDuration)
			seekDuration = time.Duration(-1)
			// Clear frame count tracking
			for _, t := range t.trackMap {
				t.lastFrameTime = 0
			}
			continue
		}

		// Find sender
		if track, ok := t.trackMap[pck.TrackNumber]; ok {
			// Only delay frames we care about
			timeDiff := pck.Timecode - time.Since(startTime)
			if timeDiff > timeEps {
				time.Sleep(timeDiff - time.Millisecond)
			}

			// Calc frame time diff per track
			diff := pck.Timecode - track.lastFrameTime
			ms := float64(diff.Milliseconds()) / 1000.0
			samps := uint32(float64(track.rate) * ms)
			track.lastFrameTime = pck.Timecode

			// Send samples
			if ivfErr := track.track.WriteSample(media.Sample{Data: pck.Data, Samples: samps}); ivfErr != nil {
				log.Infof("Track write error=%v", ivfErr)
			} else {
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

func ValidateVPFile(name string) (string, bool) {
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
