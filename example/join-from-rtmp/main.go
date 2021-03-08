package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"github.com/pion/webrtc/v3/pkg/media"
	"io"
	"net"
	"strings"
	"time"

	log "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	"github.com/pion/webrtc/v3"
	flvtag "github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
)

var ion_addr string

type RTMPHandler struct {
	rtmp.DefaultHandler
	grpc_addr string
	engine *sdk.Engine
	client *sdk.Client
	session_id string
	videoTrack, audioTrack *webrtc.TrackLocalStaticSample
}

func startRTMPServer(grpc_addr string) {
	log.Infof("Starting RTMP Server")

	tcpAddr, err := net.ResolveTCPAddr("tcp", ":1935")
	if err != nil {
		log.Panicf("Failed: %+v", err)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Panicf("Failed: %+v", err)
	}

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

	srv := rtmp.NewServer(&rtmp.ServerConfig{
		OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *rtmp.ConnConfig) {
			return conn, &rtmp.ConnConfig{
				Handler: &RTMPHandler{engine: e, grpc_addr: grpc_addr},
				ControlState: rtmp.StreamControlStateConfig{
					DefaultBandwidthWindowSize: 6 * 1024 * 1024 / 8,
				},
			}
		},
	})
	if err := srv.Serve(listener); err != nil {
		log.Panicf("Failed: %+v", err)
	}
}

func main() {
	fixByFile := []string{"asm_amd64.s", "proc.go", "icegatherer.go"}
	fixByFunc := []string{"AddProducer", "NewClient"}
	log.Init("info", fixByFile, fixByFunc)
	flag.StringVar(&ion_addr, "gaddr", "localhost:50051", "Ion-sfu grpc addr")
	go startRTMPServer(ion_addr)
	select {}
}

func (h *RTMPHandler) OnServe(conn *rtmp.Conn) {
}

func (h *RTMPHandler) OnConnect(timestamp uint32, cmd *rtmpmsg.NetConnectionConnect) error {
	log.Infof("OnConnect: %#v", cmd)
	sid := cmd.Command.App
	if sid == "/" || sid == "" || sid == "test" {
		sid = "test session"
	}
	sid = strings.Replace(sid, "/", " ", -1)
	sid = strings.Replace(sid, "%20", " ", -1)

	h.session_id = sid
	cid := "join-from-rtmp-client-id"
	h.client = h.engine.AddClient(h.grpc_addr, h.session_id, cid)
	if err := h.client.Join(h.session_id) ; err != nil {
		return errors.New("error joining session")
	}

	return nil
}

func (h *RTMPHandler) OnCreateStream(timestamp uint32, cmd *rtmpmsg.NetConnectionCreateStream) error {
	log.Infof("OnCreateStream: %#v", cmd)
	return nil
}

func (h *RTMPHandler) OnPublish(timestamp uint32, cmd *rtmpmsg.NetStreamPublish) error {
	log.Infof("OnPublish: %#v", cmd)

	if cmd.PublishingName == "" {
		return errors.New("PublishingName (stream key) is empty")
	}

	vid, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: "video/h264"}, "video", "join-from-rtmp-video",
	)
	if err != nil {
		return errors.New("error getting video track")
	}
	aud, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: "audio/opus"}, "audio", "join-from-rtmp-audio",
	)
	if err != nil {
		return errors.New("error getting audio track")
	}
	h.audioTrack = aud
	h.videoTrack = vid
	_, err = h.client.Publish(aud)
	if err != nil {
		return errors.New("error publishing audio track")
	}
	_, err = h.client.Publish(vid)
	if err != nil {
		return errors.New("error publishing video track")
	}
	return nil
}

func (h *RTMPHandler) OnAudio(timestamp uint32, payload io.Reader) error {
	var audio flvtag.AudioData
	if err := flvtag.DecodeAudioData(payload, &audio); err != nil {
		log.Errorf("error decoding audio: %s", err)
		return err
	}

	data := new(bytes.Buffer)
	if _, err := io.Copy(data, audio.Data); err != nil {
		log.Errorf("error copying audio: %s", err)
		return err
	}

	if err := h.audioTrack.WriteSample(media.Sample{
		Data:    data.Bytes(),
		Duration: 20*time.Millisecond,
	}) ; err != nil {
		log.Errorf("error sending audio: %s", err)
	}
	return nil
}

const headerLengthField = 4

func (h *RTMPHandler) OnVideo(timestamp uint32, payload io.Reader) error {
	var video flvtag.VideoData
	if err := flvtag.DecodeVideoData(payload, &video); err != nil {
		log.Errorf("error decoding video: %s", err)
		return err
	}

	data := new(bytes.Buffer)
	if _, err := io.Copy(data, video.Data); err != nil {
		log.Errorf("error copying video: %s", err)
		return err
	}
	outBuf := []byte{}
	videoBuffer := data.Bytes()
	for offset := 0; offset < len(videoBuffer); {
		bufferLength := int(binary.BigEndian.Uint32(videoBuffer[offset : offset+headerLengthField]))
		if offset+bufferLength >= len(videoBuffer) {
			break
		}

		offset += headerLengthField
		outBuf = append(outBuf, []byte{0x00, 0x00, 0x00, 0x01}...)
		outBuf = append(outBuf, videoBuffer[offset:offset+bufferLength]...)

		offset += bufferLength
	}

	if err := h.videoTrack.WriteSample(media.Sample{
		Data:    outBuf,
		Duration: time.Second / 30,
	}) ; err != nil {
		log.Errorf("error sending video: %s", err)
	}
	return nil
}

func (h *RTMPHandler) OnClose() {
	log.Infof("OnClose")
}

