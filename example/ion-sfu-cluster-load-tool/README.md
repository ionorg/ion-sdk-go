## Ion-sfu load testing tool


### Build
```
# build a linux version, we test on linux because mac fd limit 
env GOOS=linux go build -o ion-load-tool main.go
```

### Test File
Publishing of files in the following formats are supported.

|Container|Video Codecs|Audio|
|---|---|---|
WEBM|VP8|OPUS

If your data is not webm, you can use ffmpeg to make one
This show how to make a 0.5Mbps webm:

```
ffmpeg -i djrm480p.mp4 -strict -2 -b:v 0.4M -vcodec libvpx -acodec opus djrm480p.webm
```

See the ffmpeg docs on [VP8](https://trac.ffmpeg.org/wiki/Encode/VP8) for encoding options

### Quick Start
### 1 Run ion-sfu in a Linux server
You can make a script and run:

```Command Line
#!/bin/bash
ulimit -c unlimited
ulimit -SHn 1000000
sysctl -w net.ipv4.tcp_keepalive_time=60
sysctl -w net.ipv4.tcp_timestamps=0
sysctl -w net.ipv4.tcp_tw_reuse=1
#sysctl -w net.ipv4.tcp_tw_recycle=0
sysctl -w net.core.somaxconn=65535
sysctl -w net.ipv4.tcp_max_syn_backlog=65535
sysctl -w net.ipv4.tcp_syncookies=1

#your command line here, make sure run it with sudo!
./allrpc -c config.toml -gaddr "0.0.0.0:8000" -jaddr "0.0.0.0:7000" -maddr "0.0.0.0:9000" -paddr "0.0.0.0:6060"
```

### Command Line

### 2 Run ion-sfu-load-tool  in another Linux server

You can make one or two script and run:

```
#!/bin/bash
ulimit -c unlimited
ulimit -SHn 1000000
sysctl -w net.ipv4.tcp_keepalive_time=60
sysctl -w net.ipv4.tcp_timestamps=0
sysctl -w net.ipv4.tcp_tw_reuse=1
#sysctl -w net.ipv4.tcp_tw_recycle=0
sysctl -w net.core.somaxconn=65535
sysctl -w net.ipv4.tcp_max_syn_backlog=65535
sysctl -w net.ipv4.tcp_syncookies=1

#your command line here, make sure run it with sudo!
# conference mode: 10v10
# you only need one script
./ion-sfu-load-tool -file ./djrm480p.webm -clients 10 -role pubsub -gaddr "yoursfuip:8000" -session 'test' -log debug -cycle 1000



# live mode: 1v10
# you need to run two scripts
# pub.sh
#./ion-sfu-load-tool -file ./djrm480p.webm -clients 1 -role pubsub -gaddr "yoursfuip:8000" -session 'test' -log debug -cycle 1000

# sub.sh
#./ion-sfu-load-tool -file ./djrm480p.webm -clients 10 -role sub -gaddr "yoursfuip:8000" -session 'test' -log debug -cycle 1000
```


