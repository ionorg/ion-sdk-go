package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/grpc/metadata"
	"io"
	"sync"

	log "github.com/pion/ion-log"
	room "github.com/pion/ion/apps/room/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Protocol int32

const (
	Protocol_ProtocolUnknown Protocol = 0
	Protocol_WebRTC          Protocol = 1
	Protocol_SIP             Protocol = 2
	Protocol_RTMP            Protocol = 3
	Protocol_RTSP            Protocol = 4
)

type PeerState int32

const (
	PeerState_JOIN   PeerState = 0
	PeerState_UPDATE PeerState = 1
	PeerState_LEAVE  PeerState = 2
)

type Peer_Direction int32

const (
	Peer_INCOMING  Peer_Direction = 0
	Peer_OUTGOING  Peer_Direction = 1
	Peer_BILATERAL Peer_Direction = 2
)

type Role int32

const (
	Role_Host  Role = 0
	Role_Guest Role = 1
)

type RoomInfo struct {
	Sid      string
	Name     string
	Password string
	Lock     bool
}

type PeerInfo struct {
	Sid         string
	Uid         string
	DisplayName string
	ExtraInfo   []byte
	Destination string
	Role        Role
	Protocol    Protocol
	Avatar      string
	Direction   Peer_Direction
	Vendor      string
}

type JoinInfo struct {
	Sid         string
	Uid         string
	DisplayName string
	ExtraInfo   []byte
	Destination string
	Role        Role
	Protocol    Protocol
	Avatar      string
	Direction   Peer_Direction
	Vendor      string
}

type Room struct {
	Service
	connected bool
	connector *Connector

	roomServiceClient room.RoomServiceClient
	roomSignalClient  room.RoomSignalClient
	roomSignalStream  room.RoomSignal_SignalClient

	ctx    context.Context
	cancel context.CancelFunc
	sync.Mutex

	OnJoin       func(success bool, info RoomInfo, err error)
	OnLeave      func(success bool, err error)
	OnPeerEvent  func(state PeerState, Peer PeerInfo)
	OnMessage    func(from string, to string, data map[string]interface{})
	OnDisconnect func(sid, reason string)
	OnRoomInfo   func(info RoomInfo)
	OnError      func(error)
}

func GetError(err *room.Error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("[%d]: %s", err.Code, err.Reason)
}

func NewRoom(connector *Connector) *Room {
	c := &Room{
		connector: connector,
	}
	c.connector.RegisterService(c)
	if !c.Connected() {
		c.Connect()
	}
	return c
}

// CreateRoom
// Params: sid password, at lease a sid
func (r *Room) CreateRoom(info RoomInfo) error {
	if info.Sid == "" {
		return errInvalidParams
	}

	roomInfo := &room.Room{
		Sid:      info.Sid,
		Name:     info.Name,
		Password: info.Password,
		Lock:     info.Lock,
	}

	log.Infof("roomInfo=%+v", roomInfo)
	reply, err := r.roomServiceClient.CreateRoom(
		r.ctx,
		&room.CreateRoomRequest{
			Room: roomInfo,
		},
	)

	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return errReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}

func (r *Room) EndRoom(sid, reason string, delete bool) error {
	if sid == "" {
		return errInvalidParams
	}

	log.Infof("sid=%v reason=%v delete=%v", sid, reason, delete)
	reply, err := r.roomServiceClient.EndRoom(
		r.ctx,
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
		return errReplyNil
	}
	if reply.Success {
		log.Infof("reply success")
		return nil
	}
	return GetError(reply.Error)
}

// AddPeer to room, at least a sid
func (r *Room) AddPeer(peer PeerInfo) error {
	// at least sid uid
	if peer.Sid == "" || peer.Uid == "" {
		return errors.New("invalid params")
	}

	var protocolType room.Protocol
	var directionType room.Peer_Direction
	var roleType room.Role

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
	reply, err := r.roomServiceClient.AddPeer(
		r.ctx,
		&room.AddPeerRequest{
			Peer: info,
		},
	)
	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return errReplyNil
	}
	if reply.Success {
		log.Infof("reply success")
		return nil
	}
	return GetError(reply.Error)
}

func (r *Room) RemovePeer(sid, uid string) error {
	// at least sid uid
	if sid == "" || uid == "" {
		return errors.New("invalid params")
	}
	req := &room.RemovePeerRequest{
		Sid: sid,
		Uid: uid,
	}
	log.Infof("req=%+v", req)
	reply, err := r.roomServiceClient.RemovePeer(r.ctx, req)
	if err != nil {
		return err
	}
	if reply == nil {
		return errReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}

func (r *Room) UpdatePeer(peer PeerInfo) error {
	// at least sid uid
	if peer.Sid == "" || peer.Uid == "" {
		return errInvalidParams
	}

	info := &room.Peer{
		Sid:         peer.Sid,
		Uid:         peer.Uid,
		Destination: peer.Destination,
		DisplayName: peer.DisplayName,
		Role:        room.Role(peer.Role),
		Protocol:    room.Protocol(peer.Protocol),
		Direction:   room.Peer_Direction(peer.Direction),
	}
	log.Infof("info=%+v", info)
	reply, err := r.roomServiceClient.UpdatePeer(
		r.ctx,
		&room.UpdatePeerRequest{
			Peer: info,
		},
	)
	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return errReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}

func (r *Room) GetPeers(sid string) []PeerInfo {
	var infos []PeerInfo
	if sid == "" {
		return infos
	}
	reply, err := r.roomServiceClient.GetPeers(
		r.ctx,
		&room.GetPeersRequest{
			Sid: sid,
		},
	)

	if err != nil || reply == nil {
		log.Errorf("error: %v", err)
		return infos
	}
	log.Infof("peers=%+v", reply.Peers)
	for _, p := range reply.Peers {
		infos = append(infos, PeerInfo{
			Sid:         p.Sid,
			Uid:         p.Uid,
			DisplayName: p.DisplayName,
			ExtraInfo:   p.ExtraInfo,
			Destination: p.Destination,
			Role:        Role(p.Role),
			Protocol:    Protocol(p.Protocol),
			Avatar:      p.Avatar,
			Direction:   Peer_Direction(p.Direction),
			Vendor:      p.Vendor,
		})
	}
	log.Infof("infos=%+v", infos)
	return infos
}

func (r *Room) UpdateRoom(info RoomInfo) error {
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
	reply, err := r.roomServiceClient.UpdateRoom(
		r.ctx,
		&room.UpdateRoomRequest{
			Room: roomInfo,
		},
	)

	log.Infof("reply=%+v err=%v", reply, err)
	if err != nil {
		return err
	}
	if reply == nil {
		return errReplyNil
	}
	if reply.Success {
		return nil
	}
	return GetError(reply.Error)
}

func (c *Room) Close() {
	c.cancel()
	_ = c.roomSignalStream.CloseSend()
	log.Infof("Close ok")
}

func (c *Room) Join(j JoinInfo) error {
	log.Infof("join=%+v", j)

	if j.Sid == "" {
		log.Errorf("invalid sid [%v]", j.Sid)
		c.OnError(errInvalidParams)
		return errInvalidParams
	}
	if j.Uid == "" {
		j.Uid = RandomKey(6)
	}
	err := c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_Join{
				Join: &room.JoinRequest{
					Peer: &room.Peer{
						Sid:         j.Sid,
						Uid:         j.Uid,
						DisplayName: j.DisplayName,
						ExtraInfo:   j.ExtraInfo,
						Destination: j.Destination,
						Role:        room.Role(j.Role),
						Protocol:    room.Protocol(j.Protocol),
						Avatar:      j.Avatar,
						Direction:   room.Peer_Direction(j.Direction),
						Vendor:      j.Vendor,
					},
				},
			},
		},
	)
	if err != nil {
		log.Errorf("[%v] err=%v", j.Sid, err)
		c.OnError(err)
		return err
	}

	log.Infof("[%v] err=%v", j.Sid, err)
	return nil
}

// Leave from one session
func (c *Room) Leave(sid, uid string) error {
	log.Infof("uid=%v", uid)
	err := c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_Leave{
				Leave: &room.LeaveRequest{
					Sid: sid,
					Uid: uid,
				},
			},
		},
	)
	if err != nil {
		log.Errorf("[%v] err=%v", uid, err)
		c.OnError(err)
		return err
	}
	return nil
}

// SendMessage send message
// from is a uid
// to is a uid or "all"
func (c *Room) SendMessage(sid, from, to string, data map[string]interface{}) error {
	log.Infof("from=%v, to=%v, data=%v", from, to, data)
	buf, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Marshal msg.data [%v] err=%v", from, err)
		c.OnError(err)
		return err
	}
	err = c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_SendMessage{
				SendMessage: &room.SendMessageRequest{
					Sid: sid,
					Message: &room.Message{
						From:    from,
						To:      to,
						Type:    "text",
						Payload: buf,
					},
				},
			},
		},
	)
	if err != nil {
		log.Errorf("error: %v", err)
		c.OnError(err)
		return err
	}

	return nil
}

func (c *Room) roomSignalReadLoop() error {
	for {
		res, err := c.roomSignalStream.Recv()
		if err != nil {
			if err == io.EOF {
				if err := c.roomSignalStream.CloseSend(); err != nil {
					log.Errorf("error sending close: %s", err)
				}
				c.OnError(err)
				return err
			}

			errStatus, _ := status.FromError(err)
			if errStatus.Code() == codes.Canceled {
				if err := c.roomSignalStream.CloseSend(); err != nil {
					log.Errorf("error sending close: %s", err)
				}
				c.OnError(err)
				return err
			}

			log.Errorf("Error receiving room response: %v", err)
			if c.OnError != nil {
				c.OnError(err)
			}

			return err
		}

		log.Infof("Room.roomSignalReadLoop reply %v", res)

		switch payload := res.Payload.(type) {
		case *room.Reply_Join:
			reply := payload.Join
			if c.OnJoin == nil {
				log.Errorf("c.OnJoin is nil")
				continue
			}
			if reply == nil || reply.Room == nil {
				c.OnJoin(false, RoomInfo{}, fmt.Errorf("replay is nil or reply.RoomInfo is nil"))
			}
			c.OnJoin(
				reply.Success,
				RoomInfo{
					Sid:  reply.Room.Sid,
					Name: reply.Room.Name,
					Lock: reply.Room.Lock,
				},
				GetError(reply.Error),
			)

		case *room.Reply_Leave:
			reply := payload.Leave
			if c.OnLeave != nil {
				c.OnLeave(reply.Success, GetError(reply.Error))
			}
		case *room.Reply_Message:
			msg := payload.Message
			data := make(map[string]interface{})

			err := json.Unmarshal(msg.Payload, &data)
			if err != nil {
				log.Errorf("Unmarshal: err %v", err)
				c.OnError(err)
				return err
			}

			if c.OnMessage != nil {
				c.OnMessage(msg.From, msg.To, data)
			}
		case *room.Reply_Peer:
			event := payload.Peer

			if c.OnPeerEvent != nil {
				c.OnPeerEvent(
					PeerState(event.State),
					PeerInfo{
						Sid:         event.Peer.Sid,
						Uid:         event.Peer.Uid,
						DisplayName: event.Peer.DisplayName,
						ExtraInfo:   event.Peer.ExtraInfo,
						Destination: event.Peer.Destination,
						Role:        Role(event.Peer.Role),
						Protocol:    Protocol(event.Peer.Protocol),
						Avatar:      event.Peer.Avatar,
						Direction:   Peer_Direction(event.Peer.Direction),
						Vendor:      event.Peer.Vendor,
					},
				)
			}
		case *room.Reply_Disconnect:
			event := payload.Disconnect
			log.Infof("room.Disconnect: %+v", event)
			if c.OnDisconnect != nil {
				c.OnDisconnect(event.Sid, event.Reason)
			}
		case *room.Reply_Room:
			event := payload.Room
			info := RoomInfo{
				Sid:  event.Sid,
				Name: event.Name,
				Lock: event.Lock,
			}
			log.Infof("room.RoomInfo: %+v", info)
			if c.OnRoomInfo != nil {
				c.OnRoomInfo(info)
			}
		default:
			break
		}
	}
}

func (c *Room) Name() string {
	return "Room"
}

func (c *Room) Connect() {
	var err error
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.ctx = metadata.NewOutgoingContext(c.ctx, c.connector.Metadata)
	c.roomServiceClient = room.NewRoomServiceClient(c.connector.grpcConn)
	c.roomSignalClient = room.NewRoomSignalClient(c.connector.grpcConn)
	c.roomSignalStream, err = c.roomSignalClient.Signal(c.ctx)

	if err != nil {
		log.Errorf("error: %v", err)
		return
	}
	go c.roomSignalReadLoop()
	c.connected = true
	log.Infof("Room.Connect!")
}

func (c *Room) Connected() bool {
	return c.connected
}
