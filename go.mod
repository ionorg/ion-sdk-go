module github.com/pion/ion-sdk-go

go 1.15

require (
	github.com/BurntSushi/toml v0.4.1 // indirect
	github.com/ebml-go/ebml v0.0.0-20160925193348-ca8851a10894 // indirect
	github.com/ebml-go/webm v0.0.0-20160924163542-629e38feef2a
	github.com/golang/protobuf v1.5.2
	github.com/google/go-cmp v0.5.6 // indirect
	github.com/lucsky/cuid v1.2.0
	github.com/petar/GoLLRB v0.0.0-20210522233825-ae3b015fd3e9 // indirect
	github.com/pion/ice/v2 v2.1.10
	github.com/pion/ion v1.9.2-0.20210721011940-daafc471f157
	github.com/pion/ion-avp v1.8.4
	github.com/pion/ion-log v1.2.1
	github.com/pion/mediadevices v0.2.0
	github.com/pion/rtcp v1.2.6
	github.com/pion/rtp v1.6.5
	github.com/pion/sdp/v3 v3.0.4
	github.com/pion/webrtc/v3 v3.0.32
	github.com/sergi/go-diff v1.2.0 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/square/go-jose/v3 v3.0.0-20200630053402-0a67ce9b0693
	github.com/stretchr/testify v1.7.0
	golang.org/x/sys v0.0.0-20210809222454-d867a43fc93e // indirect
	golang.org/x/tools/gopls v0.7.1 // indirect
	google.golang.org/grpc v1.39.0
	google.golang.org/protobuf v1.27.1
	mvdan.cc/xurls/v2 v2.3.0 // indirect
)

replace github.com/pion/ion v1.9.2-0.20210721011940-daafc471f157 => ../ion
