package engine

import "errors"

var (
	errPeerConnectionInitFailed = errors.New("pc init failed")
	errInvalidSubscriber        = errors.New("invalid subscriber")
	errInvalidPublisher         = errors.New("invalid publisher")
	errInvalidPublishFunc       = errors.New("invalid publish func")
	errInvalidFile              = errors.New("invalid file")
	errInvalidPC                = errors.New("invalid pc")
	errInvalidKind              = errors.New("invalid kind, shoud be audio or video")
)
