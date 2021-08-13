package engine

import (
	"context"
	"errors"

	room "github.com/pion/ion/apps/room/proto"
	"github.com/pion/webrtc/v3"
)

type PeerState int32

const (
	PeerJOIN   PeerState = 0
	PeerUPDATE PeerState = 1
	PeerLEAVE  PeerState = 2
)

var (
	ErrorReplyNil      = errors.New("reply is nil")
	ErrorInvalidParams = errors.New("invalid params")
)

type RoomInfo struct {
	Sid      string
	Name     string
	Password string
	Lock     bool
}

type TrackInfo struct {
	Id       string
	StreamId string
}

type PeerInfo struct {
	Sid         string
	Uid         string
	DisplayName string
	// ExtraInfo   []byte
	Destination string
	Role        string
	Protocol    string
	// Avatar        string
	Direction string
	// Vendor        string
	Tracks []TrackInfo
}

type IonConnector struct {
	ctx      context.Context
	url      string
	engine   *Engine
	room     *RoomClient
	sfu      *Client
	uid, sid string
	pinfo    map[string]interface{}

	OnTrack       func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	OnDataChannel func(dc *webrtc.DataChannel)

	OnJoin       func(success bool, info RoomInfo, err error)
	OnLeave      func(success bool, err error)
	OnPeerEvent  func(state PeerState, Peer PeerInfo)
	OnMessage    func(from string, to string, data map[string]interface{})
	OnDisconnect func(sid, reason string)
	OnRoomInfo   func(info RoomInfo)
	OnError      func(error)
}

func NewIonConnector(addr string, uid string, pinfo map[string]interface{}) *IonConnector {
	// init engine
	i := &IonConnector{
		uid:   uid,
		url:   addr,
		pinfo: pinfo,
		room:  NewRoomClient(addr),
		ctx:   context.Background(),
	}

	// new sdk engine
	i.engine = NewEngine()

	i.room.OnJoin = func(success bool, info RoomInfo, err error) {
		log.Infof("OnJoin success=%v info=%v err=%v", success, info, err)
		if success {
			// create a new client from engine
			c, err := i.engine.NewClient(i.url, i.uid)
			if err != nil {
				log.Errorf("err=%v", err)
				return
			}

			c.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
				if i.OnTrack != nil {
					i.OnTrack(track, receiver)
				}
			}

			c.OnDataChannel = func(dc *webrtc.DataChannel) {
				if i.OnDataChannel != nil {
					i.OnDataChannel(dc)
				}
			}

			c.OnError = func(err error) {
				if i.OnError != nil {
					i.OnError(err)
				}
			}

			c.Join(i.sid)

			i.sfu = c
		}

		if i.OnJoin != nil {
			i.OnJoin(success, info, err)
		}
	}

	i.room.OnLeave = func(success bool, err error) {
		log.Infof("OnLeave success=%v err=%v", success, err)
		if i.sfu != nil {
			i.sfu.Close()
			i.sfu = nil
		}
		if i.OnLeave != nil {
			i.OnLeave(success, err)
		}
	}

	i.room.OnError = func(e error) {
		log.Infof("OnError e=%v", e)
		if i.OnError != nil {
			i.OnError(e)
		}
	}

	i.room.OnPeerEvent = func(state PeerState, Peer PeerInfo) {
		log.Infof("OnPeerEvent state=%v Peer=%v", state, Peer)
		if i.OnPeerEvent != nil {
			i.OnPeerEvent(state, Peer)
		}
	}

	i.room.OnMessage = func(from string, to string, data map[string]interface{}) {
		if i.OnMessage != nil {
			i.OnMessage(from, to, data)
		}
	}

	i.room.OnDisconnect = func(sid, reason string) {
		if i.OnDisconnect != nil {
			i.OnDisconnect(sid, reason)
		}
	}
	i.room.OnRoomInfo = func(info RoomInfo) {
		if i.OnRoomInfo != nil {
			i.OnRoomInfo(info)
		}
	}
	return i
}

func (i *IonConnector) SFU() *Client {
	return i.sfu
}

func (i *IonConnector) Join(sid string) error {
	i.sid = sid
	return i.room.Join(i.sid, i.uid, i.pinfo)
}

func (i *IonConnector) Leave(sid, uid string) error {
	return i.room.Leave(sid, uid)
}

func (i *IonConnector) Message(sid, from, to string, data map[string]interface{}) {
	i.room.SendMessage(sid, from, to, data)
}

func (i *IonConnector) Close() {
	i.room.Close()
	i.sfu.Close()
}

// CreateRoom
// Params: sid password, at lease a sid
func (i *IonConnector) CreateRoom(info RoomInfo) error {
	if info.Sid == "" {
		return ErrorInvalidParams
	}

	roomInfo := &room.Room{
		Sid:      info.Sid,
		Name:     info.Name,
		Password: info.Password,
		Lock:     info.Lock,
	}
	log.Infof("roomInfo=%+v", roomInfo)
	reply, err := i.room.CreateRoom(i.ctx,
		&room.CreateRoomRequest{
			Room: roomInfo,
		},
	)
	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return ErrorReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}

func (i *IonConnector) EndRoom(sid, reason string, delete bool) error {
	if sid == "" {
		return ErrorInvalidParams
	}

	log.Infof("sid=%v reason=%v delete=%v", sid, reason, delete)
	reply, err := i.room.EndRoom(
		i.ctx,
		&room.EndRoomRequest{
			Sid:    sid,
			Reason: reason,
			Delete: delete,
		},
	)
	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return ErrorReplyNil
	}
	if reply.Success {
		log.Infof("reply success")
		return nil
	}
	return GetError(reply.Error)
}

// AddPeer to room, at least a sid
// sid: session/room id
// uid: user/peer id
// dest: url if is rtmp/rtsp/sip..
// name: source name if is rtmp/rtsp/sip..
// protocol: webrtc/rtmp/rtsp/sip  default: webrtc
// direction: push/pull/both
// role: host/guest
//func (i *IonConnector) AddPeer(sid, uid, dest, name, protocol, direction, role ...string) error {
//func (i *IonConnector) AddPeer(args ...string) error {
func (i *IonConnector) AddPeer(peer PeerInfo) error {
	// at least sid uid
	if peer.Sid == "" || peer.Uid == "" {
		return errors.New("invalid params")
	}

	var protocolType room.Protocol
	var directionType room.Peer_Direction
	var roleType room.Role

	switch peer.Protocol {
	case "rtmp":
		protocolType = room.Protocol_RTMP
	case "rtsp":
		protocolType = room.Protocol_RTSP
	case "sip":
		protocolType = room.Protocol_SIP
	default:
		protocolType = room.Protocol_WebRTC
	}

	switch peer.Direction {
	case "push":
		directionType = room.Peer_INCOMING
	case "pull":
		directionType = room.Peer_OUTGOING
	}

	switch peer.Role {
	case "host":
		roleType = room.Role_Host
	case "guest":
		roleType = room.Role_Guest
	}

	info := &room.Peer{
		Sid:         peer.Sid,
		Uid:         peer.Uid,
		Destination: peer.Destination,
		DisplayName: peer.DisplayName,
		Role:        roleType,
		Protocol:    protocolType,
		Direction:   directionType,
	}
	log.Infof("info=%+v", info)
	reply, err := i.room.AddPeer(
		i.ctx,
		&room.AddPeerRequest{
			Peer: info,
		},
	)
	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return ErrorReplyNil
	}
	if reply.Success {
		log.Infof("reply success")
		return nil
	}
	return GetError(reply.Error)
}

func (i *IonConnector) RemovePeer(sid, uid string) error {
	// at least sid uid
	if sid == "" || uid == "" {
		return errors.New("invalid params")
	}
	req := &room.RemovePeerRequest{
		Sid: sid,
		Uid: uid,
	}
	log.Infof("req=%+v", req)
	reply, err := i.room.RemovePeer(i.ctx, req)
	if err != nil {
		return err
	}
	if reply == nil {
		return ErrorReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}

func (i *IonConnector) UpdatePeer(peer PeerInfo) error {
	// at least sid uid
	if peer.Sid == "" || peer.Uid == "" {
		return errors.New("invalid params")
	}

	var protocolType room.Protocol
	var directionType room.Peer_Direction
	var roleType room.Role

	switch peer.Protocol {
	case "rtmp":
		protocolType = room.Protocol_RTMP
	case "rtsp":
		protocolType = room.Protocol_RTSP
	case "sip":
		protocolType = room.Protocol_SIP
	default:
		protocolType = room.Protocol_WebRTC
	}

	switch peer.Direction {
	case "push":
		directionType = room.Peer_INCOMING
	case "pull":
		directionType = room.Peer_OUTGOING
	}

	switch peer.Role {
	case "host":
		roleType = room.Role_Host
	case "guest":
		roleType = room.Role_Guest
	}
	info := &room.Peer{
		Sid:         peer.Sid,
		Uid:         peer.Uid,
		Destination: peer.Destination,
		DisplayName: peer.DisplayName,
		Role:        roleType,
		Protocol:    protocolType,
		Direction:   directionType,
	}
	log.Infof("info=%+v", info)
	reply, err := i.room.UpdatePeer(
		i.ctx,
		&room.UpdatePeerRequest{
			Peer: info,
		},
	)
	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return ErrorReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}

func (i *IonConnector) GetPeers(sid string) []PeerInfo {
	var infos []PeerInfo
	if sid == "" {
		return infos
	}
	reply, err := i.room.GetPeers(
		i.ctx,
		&room.GetPeersRequest{
			Sid: sid,
		},
	)

	if err != nil || reply == nil {
		log.Errorf("err=%v", err)
		return infos
	}
	log.Infof("peers=%+v", reply.Peers)
	for _, p := range reply.Peers {
		var trackInfos []TrackInfo
		for _, t := range p.Tracks {
			trackInfos = append(trackInfos, TrackInfo{
				Id:       t.Id,
				StreamId: t.StreamId,
			})
		}
		infos = append(infos, PeerInfo{
			Sid:    p.Sid,
			Uid:    p.Uid,
			Tracks: trackInfos,
		})
	}
	log.Infof("infos=%+v", infos)
	return infos
}

func (i *IonConnector) UpdateRoom(info RoomInfo) error {
	if info.Sid == "" {
		return errors.New("invalid params")
	}

	roomInfo := &room.Room{
		Sid:      info.Sid,
		Name:     info.Name,
		Lock:     info.Lock,
		Password: info.Password,
	}
	log.Infof("roomInfo=%+v", roomInfo)
	reply, err := i.room.UpdateRoom(
		i.ctx,
		&room.UpdateRoomRequest{
			Room: roomInfo,
		},
	)

	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return ErrorReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}
