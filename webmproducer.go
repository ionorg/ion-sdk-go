package engine

import (
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
	videoTrack    *webrtc.TrackLocalStaticSample
	audioTrack    *webrtc.TrackLocalStaticSample
	offsetSeconds int
	reader        *webm.Reader
	webm          webm.WebM
	trackMap      map[uint]*trackInfo
	videoCodec    string
	file          *os.File
	sendByte      int
	id            string
}

// NewWebMProducer new a WebMProducer
func NewWebMProducer(id, name string, offset int) *WebMProducer {
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
		id:            id,
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

func (t *WebMProducer) AudioTrack() *webrtc.TrackLocalStaticSample {
	return t.audioTrack
}

func (t *WebMProducer) VideoTrack() *webrtc.TrackLocalStaticSample {
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
func (t *WebMProducer) AddTrack(pc *webrtc.PeerConnection, kind string) (*webrtc.TrackLocalStaticSample, error) {
	if pc == nil {
		return nil, errInvalidPC
	}
	if kind != "video" && kind != "audio" {
		return nil, errInvalidKind
	}

	var track *webrtc.TrackLocalStaticSample
	var err error
	// streamId := fmt.Sprintf("%d", time.Now().UnixNano())
	streamId := fmt.Sprintf("webm_%p", t)
	if kind == "video" {
		if vTrack := t.webm.FindFirstVideoTrack(); vTrack != nil {

			var track *webrtc.TrackLocalStaticSample
			switch vTrack.CodecID {
			case "V_VP8":
				t.videoCodec = webrtc.MimeTypeVP8
				track, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "video", streamId)
			case "V_VP9":
				t.videoCodec = webrtc.MimeTypeVP9
				track, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9, ClockRate: 90000}, "video", streamId)
			default:
				log.Errorf("Unsupported video codec %v", vTrack.CodecID)
			}

			if err != nil {
				panic(err)
			}

			// _, err = pc.AddTrack(track)
			_, err = pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionSendonly,
			})
			if err != nil {
				log.Errorf("err=%v", err)
				return nil, err
			}
			t.trackMap[vTrack.TrackNumber] = &trackInfo{track: track, rate: 90000}
			t.videoTrack = track
		}
	} else if kind == "audio" {
		if aTrack := t.webm.FindFirstAudioTrack(); aTrack != nil {
			track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", streamId)

			if err != nil {
				panic(err)
			}

			// _, err = pc.AddTrack(track)
			_, err = pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionSendonly,
			})
			if err != nil {
				log.Errorf("err=%v", err)
				return nil, err
			}
			t.trackMap[aTrack.TrackNumber] = &trackInfo{
				track: track,
				rate:  int(aTrack.Audio.OutputSamplingFrequency),
			}
			t.audioTrack = track
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
				log.Tracef("id=%v mime=%v kind=%v streamid=%v len=%v", t.id, track.track.Codec().MimeType, track.track.Kind(), track.track.StreamID(), len(pck.Data))
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
