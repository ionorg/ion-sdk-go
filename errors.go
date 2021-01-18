package engine

import "errors"

var (
	errInvalidClientID = errors.New("invalid client id")
	errInvalidSessID   = errors.New("invalid session id")
	errInvalidFile     = errors.New("invalid file")
	errInvalidPC       = errors.New("invalid pc")
	errInvalidKind     = errors.New("invalid kind, shoud be audio or video")
)
