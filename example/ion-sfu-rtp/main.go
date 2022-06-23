package main

import (
	"os"
	"fmt"
	"flag"
	"net"
	"io"
	"time"
	"syscall"
	"sync"
	"os/signal"

	ilog "github.com/pion/ion-log"
	sdk "github.com/pion/ion-sdk-go"
	rtp "github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

// media info
type minfo struct {
	nr_rtp int
	trackid string
	streamid string
	mime string
	mtype string
	fwdport int
	pt uint8
	seq uint16
	ts uint32
	ssrc uint32
	nr_pkt_lost int
	conn *net.UDPConn
}

// main configuration
type main_data struct {
	room *sdk.Room
	rtc *sdk.RTC
	session, addr, gst_ip, log_level, uid, rid, client_id string
	video_mtype, audio_mtype, vcodec_type, acodec_type string
	pid int
	nr_stream int
	at1, at2, vt1, vt2 *webrtc.TrackLocalStaticRTP
	al1, al2, vl1, vl2 *net.UDPConn
}

const (
	listen_audio1      = 21000
	listen_video1      = 21002
	listen_audio2      = 21004
	listen_video2      = 21006
	print_count        = 1000000
	stat_interval      = 10
	delay_stream       = 10
	datefmt            = "20060102:150405.999999Z07:00"
)

var (
	md main_data
	listen_ip1 string  = "225.0.0.225"
	forward_ip1 string = "225.0.0.225"
	rtp_fwd int        = 0
	fwd_audio_cur int  = 22000
	fwd_video_cur int  = 22002
	log = ilog.NewLoggerWithFields(ilog.TraceLevel, "ion-sfu-rtp", nil)
	lmap map[string]*minfo
	rmap map[string]*minfo
	lmutex sync.RWMutex
	rmutex sync.RWMutex
)

func print_stat() {
	nr := 0
	log.Infof("Printing Stat %s", time.Now().Format(datefmt))
	fmt.Printf("\n")
	fmt.Printf("***********\n")
	fmt.Printf("Printing Local Stat\n")
	fmt.Printf("***********\n")
	for n, mi := range lmap {
		m := fmt.Sprintf("[%2d] [%s] rtp:%-5d mtype:%-10s " +
			"fwd:%-5d pt:%-3d ssrc:%08x:%-10d pktlost:%d\n",
			nr, n, mi.nr_rtp, mi.mtype,
			mi.fwdport, mi.pt, mi.ssrc, mi.ssrc, mi.nr_pkt_lost)
		fmt.Printf(m)
		nr++
	}
	fmt.Printf("\n")
	nr = 0
	fmt.Printf("***********\n")
	fmt.Printf("Printing Remote Stat\n")
	fmt.Printf("***********\n")
	for n, mi := range rmap {
		m := fmt.Sprintf("[%2d] [%s] rtp:%-5d mtype:%-10s " +
			"fwd:%-5d pt:%-3d ssrc:%08x:%-10d pktlost:%d\n",
			nr, n, mi.nr_rtp, mi.mtype,
			mi.fwdport, mi.pt, mi.ssrc, mi.ssrc, mi.nr_pkt_lost)
		fmt.Printf(m)
		nr++
	}
	fmt.Printf("***********\n")
	fmt.Printf("\n")
}

func write_chat() {
	var payload map[string]interface{}
	var msg map[string]string

	dname := md.rid + "_name"
	tm := time.Now().Format("15:04:05.999")
	txt := fmt.Sprintf("from %s at %s", md.rid, tm)
	payload = make(map[string]interface{})
	msg = make(map[string]string)
	msg["uid"] = md.rid
	msg["name"] = dname
	msg["text"] = txt
	payload["msg"] = msg
	err := md.room.SendMessage(md.session, dname, "all", payload)
	if err != nil {
		panic(err)
	}
	return
}

func forward_rtp_packet(mi *minfo, b []uint8, n int) {
	if(mi.conn == nil) {
		rem := fmt.Sprintf("%s:%d", forward_ip1, mi.fwdport)
		remaddr, err := net.ResolveUDPAddr("udp", rem)
		log.Infof("forward_rtp_packet: remote %+v\n", remaddr)
		if err != nil {
			panic(err)
		}
		mi.conn, err = net.DialUDP("udp", nil, remaddr)
		if err != nil {
			panic(err)
		}
	}
	n1, err := mi.conn.Write(b[:n])
	if err != nil {
		/* panic(err) */
	}
	if n != n1 {
		/* panic(err) */
	}
	//log.Infof("mi %+v, len %d %d\n", mi, n, n1)

	return
}

func analyze_rtp_packet(mi *minfo, b []uint8, n int) {
	var nr_lost uint16

	h := &rtp.Header{}
	h.Unmarshal(b)

	if(mi.nr_rtp == 0) {
		mi.pt = h.PayloadType
		mi.ssrc = h.SSRC
		mi.seq = h.SequenceNumber - 1
		mi.ts = h.Timestamp
	}
	nr_lost = h.SequenceNumber - mi.seq - 1
	mi.nr_pkt_lost += int(nr_lost)
	mi.seq = h.SequenceNumber
	mi.ts = h.Timestamp
	mi.nr_rtp++
	if(mi.pt != h.PayloadType) {
		log.Errorf("Payload Type Changed from %d to %d %s",
			mi.pt, h.PayloadType, mi.trackid)
		mi.pt = h.PayloadType
	}
	if(mi.ssrc != h.SSRC) {
		log.Errorf("SSRC Changed from %d to %d %08x %s",
			mi.ssrc, h.SSRC, h.SSRC, mi.trackid)
		mi.ssrc = h.SSRC
	}
	if(mi.nr_rtp < 10) {
		log.Infof(">> [%5d %5d] - %s - %x %08x %5d %d\n",
			mi.nr_rtp, n, mi.trackid, h.PayloadType, h.SSRC,
			h.SequenceNumber, h.Timestamp)
	}

	return
}

func process_local_rtp(name string, uid string, mtype string,
	listener *net.UDPConn, track *webrtc.TrackLocalStaticRTP) () {
	var mi *minfo

	streamid := track.StreamID()
	key := streamid + "_" + name
	lmutex.Lock()
	mi, ok := lmap[key]
	lmutex.Unlock()
	if(!ok) {
		mi = new(minfo)
		mi.trackid = name
		mi.streamid = streamid
		mi.mtype = mtype
		lmutex.Lock()
		lmap[key] = mi
		lmutex.Unlock()
	}

	b := make([]byte, 1600) // UDP MTU
	for {
		n, _, err := listener.ReadFrom(b)
		if err != nil {
			panic(fmt.Sprintf("error during read: %s", err))
		}
		n1, err := track.Write(b[:n])
		if err != nil {
			log.Infof("RTP %s - %d - %d - %+v",  name, n, n1, err)
		}
		analyze_rtp_packet(mi, b, n)
		if((mi.nr_rtp % print_count) == 0) {
			log.Infof("LOCAL %s [%d] [%s %s] %5d",
				uid, mi.nr_rtp, streamid, name, n1)
		}
	}

	return
}

func read_remote_rtp(uid string, rtc *sdk.RTC) {
	rtc.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		ssrc := track.SSRC();
		m := fmt.Sprintf("read_remote_rtp() OnTrack " +
			"streamId=%v trackId=%v kind=%v ssrc=%d(%08x) ",
			track.StreamID(), track.ID(), track.Kind(), ssrc, ssrc)
		log.Infof(m)

		b := make([]byte, 1600)
		for {
			select {
			default:
				n, _, err := track.Read(b)
				if err != nil {
					if err == io.EOF {
						log.Errorf("id=%v track.ReadRTP err=%v", rtc, err)
						return
					}
					log.Errorf("id=%v Error reading track rtp %s", rtc, err)
					continue
				}
				streamid := track.StreamID()
				trackid := track.ID()
				mime := track.Codec().RTPCodecCapability.MimeType
				key := streamid + "_" + trackid
				rmutex.Lock()
				mi, ok := rmap[key]
				rmutex.Unlock()
				if(!ok) {
					mi = new(minfo)
					mi.trackid = trackid
					mi.streamid = streamid
					mi.mime = mime
					mi.mtype = mime[0:5]
					rmutex.Lock()
					if(mi.mtype == "audio") {
						mi.fwdport = fwd_audio_cur
						fwd_audio_cur += 4
					} else {
						mi.fwdport = fwd_video_cur
						fwd_video_cur += 4
					}
					rmap[key] = mi
					rmutex.Unlock()
				}
				switch(mi.mime) {
				case sdk.MimeTypeVP8:
					break
				case sdk.MimeTypeVP9:
					break
				case sdk.MimeTypeH264:
					break
				case sdk.MimeTypeOpus:
					break
				default:
					break
				}
				analyze_rtp_packet(mi, b, n)
				if((mi.nr_rtp % print_count) == 0) {
				log.Infof("REMOTE %s [%d] [%s %s] %5d, %s",
					uid, mi.nr_rtp, streamid, trackid, n, mime)
				}
				if(rtp_fwd != 0) {
					forward_rtp_packet(mi, b, n)
				}
			}
		}
	}
}

func create_local_track(streamid_audio, streamid_video,
	trackid_audio, trackid_video, acodec_type, vcodec_type string) (
	audio_track, video_track *webrtc.TrackLocalStaticRTP) {
	video_track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: "video/" + vcodec_type},
		trackid_video, streamid_video)
	if err != nil {
		panic(err)
	}

	audio_track, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: "audio/" + acodec_type},
		trackid_audio, streamid_audio)
	if err != nil {
		panic(err)
	}

	return
}

func create_local_listener(ip string,
	aport, vport int) (aconn, vconn *net.UDPConn) {
	aconn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(ip),
		Port: aport})
	if err != nil {
		panic(err)
	}
	vconn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(ip),
		Port: vport})
	if err != nil {
		panic(err)
	}

	return
}

func rtc_main(connector *sdk.Connector) {
	// new sdk engine
	config := sdk.RTCConfig{
		WebRTC: sdk.WebRTCTransportConfig{
			VideoMime: md.video_mtype,
		},
	}

	rtc, err := sdk.NewRTC(connector, config)
	if err != nil {
		panic(err)
	}
	md.rtc = rtc

	// keeping an easily identifiable unique id by concatanating
	// the given client_id and the process id.
	streamid_audio1 := fmt.Sprintf("p1_%s", md.uid)
	streamid_video1 := fmt.Sprintf("p1_%s", md.uid)
	streamid_audio2 := fmt.Sprintf("p2_%s", md.uid)
	streamid_video2 := fmt.Sprintf("p2_%s", md.uid)
	trackid_audio1 := fmt.Sprintf("a1_%s", md.uid)
	trackid_video1 := fmt.Sprintf("v1_%s", md.uid)
	trackid_audio2 := fmt.Sprintf("a2_%s", md.uid)
	trackid_video2 := fmt.Sprintf("v2_%s", md.uid)
	log.Infof("main [%s]: remote %s", md.uid, md.addr)
	log.Infof("main [%s]: session %s", md.uid, md.session)
	log.Infof("main [%s]: gst_ip %s %s %s",
		md.uid, md.gst_ip, listen_ip1, forward_ip1)
	log.Infof("main [%s]: log_level %s", md.uid, md.log_level)
	log.Infof("main [%s]: nr_stream %d", md.uid, md.nr_stream)
	log.Infof("main [%s]: rtp_fwd %d", md.uid, rtp_fwd)
	log.Infof("main [%s]: %s, %s", md.uid, streamid_audio1, streamid_video1)
	log.Infof("main [%s]: %s, %s", md.uid, streamid_audio2, streamid_video2)
	log.Infof("main [%s]: %s, %s", md.uid, trackid_audio1, trackid_video1)
	log.Infof("main [%s]: %s, %s", md.uid, trackid_audio2, trackid_video2)

	read_remote_rtp(md.uid, rtc)
	rtc.OnDataChannel = func(dc *webrtc.DataChannel) {
		log.Infof("main [%s]: OnDataChannel", md.uid)
	}

	// client join a session
	err = rtc.Join(md.session, md.rid)
	if err != nil {
		log.Errorf("rtc_main: join err=%v", err)
		panic(err)
	}

	if(md.nr_stream > 0) {
		log.Infof("main [%s]: Publishing First Track", md.uid)
		md.at1, md.vt1 = create_local_track(streamid_audio1, streamid_video1,
			trackid_audio1, trackid_video1, md.acodec_type, md.vcodec_type)
		md.al1, md.vl1 = create_local_listener(listen_ip1,
			listen_audio1, listen_video1)
		log.Infof("main [%s]: listener [%+v %+v]\n%+v\n%+v",
			md.uid, md.al1, md.vl1, md.at1, md.vt1)

		_, _ = rtc.Publish(md.vt1, md.at1)
		go process_local_rtp(trackid_audio1, md.uid, md.audio_mtype,
			md.al1, md.at1)
		go process_local_rtp(trackid_video1, md.uid, md.video_mtype,
			md.vl1, md.vt1)
	}

	if(md.nr_stream > 1) {
		delay := delay_stream
		for i := 0; i < delay; i++ {
			log.Infof("main [%s]: Publishing Second Stream in %d",
				md.uid, (delay -i))
			time.Sleep(time.Millisecond * 1000)
		}
		log.Infof("main [%s]: Publishing Second Stream", md.uid)
		md.at2, md.vt2 = create_local_track(streamid_audio2, streamid_video2,
			trackid_audio2, trackid_video2, md.acodec_type, md.vcodec_type)
		md.al2, md.vl2 = create_local_listener(listen_ip1,
			listen_audio2, listen_video2)
		log.Infof("main [%s]: listener [%+v %+v]\n%+v\n%+v",
			md.uid, md.al2, md.vl2, md.at2, md.vt2)

		_, _ = rtc.Publish(md.vt2, md.at2)
		go process_local_rtp(trackid_audio2, md.uid, md.audio_mtype, md.al2, md.at2)
		go process_local_rtp(trackid_video2, md.uid, md.video_mtype, md.vl2, md.vt2)
	} else {
		log.Infof("main [%s]: No Second Track", md.uid)
	}

	return
}

func room_main(connector *sdk.Connector) {
	dname := md.rid + "_name"
	room := sdk.NewRoom(connector)
	md.room = room

	err := room.CreateRoom(sdk.RoomInfo{Sid: md.session})
	if err != nil {
		log.Errorf("room_main: err=%v", err)
		panic(err)
	}

	err = room.AddPeer(sdk.PeerInfo{Sid: md.session, Uid: md.rid})
	if err != nil {
		log.Errorf("room_main: err=%v", err)
		panic(err)
	}

	err = room.UpdatePeer(sdk.PeerInfo{Sid: md.session,
		Uid: md.rid, DisplayName: dname})
	if err != nil {
		log.Errorf("room_main: err=%v", err)
		panic(err)
	}
	peers := room.GetPeers(md.session)
	log.Infof("room_main: peers %d", len(peers))
	for n, p := range peers {
		log.Infof("room_main: peer [%2d] = %+v", n, p)
	}

	err = room.UpdateRoom(sdk.RoomInfo{Sid: md.session, Lock: true})
	if err != nil {
		log.Errorf("room_main: err=%v", err)
		panic(err)
	}

	err = room.UpdateRoom(sdk.RoomInfo{Sid: md.session, Lock: false})
	if err != nil {
		log.Errorf("room_main: err=%v", err)
		panic(err)
	}

	/*
	err = room.EndRoom(md.session, "conference end", true)
	if err != nil {
		log.Errorf("room_main: err=%v", err)
		panic(err)
	}
	*/

	room.OnJoin = func(success bool, info sdk.RoomInfo, err error) {
		log.Infof("room_main: OnJoin success %+v, info = %+v",
			success, info)
		// calling rtc_main one room join is done
		rtc_main(connector)
	}

	room.OnLeave = func(success bool, err error) {
		log.Infof("room_main: OnLeave success %+v", success)
	}

	room.OnPeerEvent = func(state sdk.PeerState, peer sdk.PeerInfo) {
		log.Infof("room_main: OnPeerEvent state %+v, peer = %+v",
			state, peer)
	}

	room.OnMessage = func(from, to string, data map[string]interface{}) {
		log.Infof("room_main: OnMessage from %+v, to = %+v, data = %+v",
			from, to, data)
	}

	room.OnDisconnect = func(sid, reason string) {
		log.Infof("room_main: OnDisconnect sid = %+v, reason = %+v",
			sid, reason)
	}

	room.OnRoomInfo = func(info sdk.RoomInfo) {
		log.Infof("room_main: OnRoomInfo info = %+v", info)
	}

	jinfo := sdk.JoinInfo{
		Sid: md.session,
		Uid: md.rid,
		DisplayName: dname,
		Role: sdk.Role_Host,
		Protocol: sdk.Protocol_WebRTC,
		Direction: sdk.Peer_BILATERAL,
	}
	err = room.Join(jinfo)
	return
}

func exit_cleanup() {
	fmt.Printf("exit_cleanup:\n")
	if(md.room != nil) {
		md.room.Leave(md.session, md.rid)
	}
	time.Sleep(time.Millisecond * 1000)
	return
}

func main() {
	// parse flag
	flag.StringVar(&md.addr, "addr", "localhost:5551", "ion-sfu grpc addr")
	flag.StringVar(&md.session, "session", "ion", "join session name")
	flag.StringVar(&md.gst_ip, "gst_ip", "null",
		"gstreamer ip to listen or forward")
	flag.StringVar(&md.log_level, "log", "info",
		"log level:debug|info|warn|error")
	flag.StringVar(&md.client_id, "client_id", "cl0",
		"client id of this client")
	flag.IntVar(&md.nr_stream, "nr_stream", 1, "number of streams 1 or 2")
	flag.IntVar(&rtp_fwd, "rtp_fwd", 0, "rtp_fwd 0 or 1")
	flag.Parse()
	if(md.gst_ip != "null") {
		listen_ip1 = md.gst_ip
		forward_ip1 = md.gst_ip
	}
	md.acodec_type = "opus"
	md.audio_mtype = sdk.MimeTypeOpus
	vcodec_env := os.Getenv("VCODEC")
	if(vcodec_env == "H264") {
		md.video_mtype = sdk.MimeTypeH264
		md.vcodec_type = "h264"
	} else {
		md.video_mtype = sdk.MimeTypeVP8
		md.vcodec_type = "vp8"
	}
	sigchan := make(chan os.Signal, 1)
	donechan := make(chan bool, 1)
	do_exit := 0
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	lmap = make(map[string]*minfo)
	rmap = make(map[string]*minfo)

	connector := sdk.NewConnector(md.addr)
	md.pid = os.Getpid()
	md.uid = fmt.Sprintf("%s_%d", md.client_id, md.pid)
	md.rid = sdk.RandomKey(6)
	fmt.Printf("[%s %s] %+v\n", md.uid, md.rid, connector)
	room_main(connector)
	// calling rtc_main directly without room join
	//rtc_main(connector)

	m := fmt.Sprintf("main [%s]: waiting", md.uid)
	log.Infof(m)
	fmt.Printf("%s\n", m)
	time.Sleep(time.Millisecond * 1000)
	tick1 := time.NewTicker(stat_interval * time.Second)

	go func () {
		sig := <- sigchan
		fmt.Printf("***********\n")
		fmt.Printf("main [%s]: signal received [%+v]\n", md.uid, sig)
		fmt.Printf("***********\n")
		print_stat()
		exit_cleanup()
		donechan <- true
		do_exit = 1
	}()

	for do_exit == 0 {
		select {
			case <- tick1.C:
				fmt.Printf("main [%s]: tick\n", md.uid)
				write_chat()
				print_stat()
			case done :=  <-donechan:
				fmt.Printf("main [%s]: done received %+v\n", md.uid, done)
				tick1.Stop()
				break
		}
	}

	fmt.Printf("main [%s]: exiting\n", md.uid)

	return
}

