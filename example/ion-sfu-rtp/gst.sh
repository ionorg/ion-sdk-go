#!/bin/bash

#set -x
#set -e

tx_port_begin=${TX_PORT_BEGIN:-21000}
rx_port_begin=${RX_PORT_BEGIN:-22000}

# this same default multicast address is used in main.go too
default_ip=${DEFAULT_IP:-225.0.0.225}

# video parameters, width, height, fps etc.
w=${WIDTH:-640}
h=${HEIGHT:-360}
fps=${FPS:-15}
# video bitrate and audio bitrate
vb=${VB:-256000}
ab=${AB:-20000}
vb_kbps=$((vb / 1000))

# use the v4l2-ctl command to find out the formats supported
# v4l2-ctl -d /dev/video0 --list-formats-ext
v4l2_device=${V4L2_DEVICE:-/dev/video0}

gst='gst-launch-1.0'
#gst="${gst} -v"

sigint()
{
  echo "Received SIGINT"
  sleep 1
  ps -f | grep ${gst}
  ps -f | grep ${gst} | awk '{print $2}' | xargs kill
}

sigterm()
{
  echo "Received SIGTERM"
  sleep 1
  ps -f | grep ${gst}
  ps -f | grep ${gst} | awk '{print $2}' | xargs kill
}


# pattern 0 smpte, 18 moving ball
video_tx_vp8()
{
  echo "video tx vp8"
  port=${1}
  pattern=${2}
  if [ "${SRC}" == "v4l2" ];
  then
    videosrc="v4l2src device=${v4l2_device}"
    capsrc="video/x-raw,format=YUY2,width=${w},height=${h},framerate=${fps}/1"
  else
    videosrc="videotestsrc pattern=${pattern}"
    capsrc="video/x-raw,format=I420,width=${w},height=${h},framerate=${fps}/1"
  fi
  ${gst} \
    ${videosrc} ! ${capsrc} ! \
    videoscale ! videorate ! videoconvert ! \
    timeoverlay halignment=center valignment=center ! \
    vp8enc error-resilient=partitions keyframe-max-dist=10 auto-alt-ref=true cpu-used=5 deadline=1 target-bitrate=${vb} ! \
    rtpvp8pay pt=120 ! udpsink host=${remote_ip} port=${port} &
}

video_tx_h264() {
  echo "video tx h264"
  port=${1}
  pattern=${2}
  if [ "${SRC}" == "v4l2" ];
  then
    videosrc="v4l2src device=${v4l2_device}"
    capsrc="video/x-raw,format=YUY2,width=${w},height=${h},framerate=${fps}/1"
  else
    videosrc="videotestsrc pattern=${pattern}"
    capsrc="video/x-raw,format=I420,width=${w},height=${h},framerate=${fps}/1"
  fi
  ${gst} \
    ${videosrc} ! ${capsrc} ! \
    videoscale ! videorate ! videoconvert ! \
    timeoverlay halignment=center valignment=center ! \
    x264enc bframes=0 cabac=0 dct8x8=0 speed-preset=ultrafast tune=zerolatency key-int-max=20 bitrate=${vb_kbps} ! video/x-h264,stream-format=byte-stream ! \
    rtph264pay pt=126 ! udpsink host=${remote_ip} port=${port} &
}

# wave 0 sine, 8 ticks
# when multiple streams are used, select frequency very near so that
# audio beats are there
# this is a way of distinguishing different streams easily.
audio_tx_opus()
{
  echo "audio tx opus"
  port=${1}
  wave=${2}
  freq=${3}
  ${gst} \
    audiotestsrc wave=${wave} freq=${freq} ! audioresample ! audio/x-raw,channels=1,rate=48000 ! \
    opusenc bitrate=${ab} ! rtpopuspay pt=109 ! udpsink host=${remote_ip} port=${port} &
}

video_rx_vp8()
{
  echo "video rx vp8"
  port=${1}
  ${gst} \
    udpsrc address=${listen_ip} port=${port} \
    caps='application/x-rtp, media=(string)video, clock-rate=(int)90000' ! \
    rtpvp8depay ! vp8dec ! \
    videoconvert ! autovideosink &
}

video_rx_h264()
{
  echo "video rx h264"
  port=${1}
  #decoder=decodebin
  decoder=openh264dec
  ${gst} \
    udpsrc address=${listen_ip} port=${port} \
    caps='application/x-rtp, media=(string)video, clock-rate=(int)90000' ! \
    rtph264depay ! h264parse ! ${decoder} ! \
    videoconvert ! autovideosink
}

audio_rx_opus()
{
  echo "audio rx opus"
  port=${1}
  ${gst} \
    udpsrc address=${listen_ip} port=${port} \
    caps="application/x-rtp, media=(string)audio" ! \
    rtpopusdepay ! opusdec ! autoaudiosink &
}

set_udp_port()
{
  let audio_tx_port1=${tx_port_begin}
  let video_tx_port1=${tx_port_begin}+2
  let audio_rx_port1=${rx_port_begin}
  let video_rx_port1=${rx_port_begin}+2

  let audio_tx_port2=${tx_port_begin}+4
  let video_tx_port2=${tx_port_begin}+6
  let audio_rx_port2=${rx_port_begin}+4
  let video_rx_port2=${rx_port_begin}+6
}

print_info() {
  echo ""
  echo "mod            = ${mod}"
  echo "nr_stream      = ${nr_stream}"
  echo "remote_ip      = ${remote_ip}"
  echo "listen_ip      = ${listen_ip}"
  echo "tx_port_begin  = ${tx_port_begin}"
  echo "rx_port_begin  = ${rx_port_begin}"
  echo "audio_tx_port1 = ${audio_tx_port1}"
  echo "video_tx_port1 = ${video_tx_port1}"
  echo "audio_rx_port1 = ${audio_rx_port1}"
  echo "video_rx_port1 = ${video_rx_port1}"
  echo "audio_tx_port2 = ${audio_tx_port2}"
  echo "video_tx_port2 = ${video_tx_port2}"
  echo "audio_rx_port2 = ${audio_rx_port2}"
  echo "video_rx_port2 = ${video_rx_port2}"
  echo ""
}

usage()
{
  echo
  echo "$0 <-d|-e> <nr_stream 1|2> [remote_ip|listen_ip] [port]"
  echo
  echo "-d = decode mode"
  echo "-e = encode mode"
  echo "remote_ip, listen_ip optional, default multicast ${default_ip}"
  echo "port is either remote send port or local listen port"
  echo "port must be preceded with ip argument"
  echo
  echo "To use H.264 codec for both decode and encode, use the environment"
  echo "export VCODEC=H264"
  echo "To use v4l2 camera instead of videotestsrc use the environment"
  echo "v4l2 works only with 1 stream"
  echo "export SRC=v4l2"
  echo "The default video codec is VP8"
  echo
}

if [ "$#" -lt "2" ];
then
  usage
  exit 2
fi

mod=${1}
nr_stream=${2}
shift 2
if [ "$#" -ne "0" ] && [ "$#" -ne "2" ];
then
  echo "Both IP and Port must be specified"
  usage
  exit 2
fi
ip=${1}
port=${2}
if [ "${ip}" == "" ];
then
  remote_ip=${default_ip}
  listen_ip=${default_ip}
else
  remote_ip=${ip}
  listen_ip=${ip}
fi
if [ "${port}" != "" ];
then
  tx_port_begin=${port}
  rx_port_begin=${port}
fi
set_udp_port
print_info
sleep 1

if [ "${VCODEC}" == "H264" ];
then
  video_tx=video_tx_h264
  video_rx=video_rx_h264
else
  video_tx=video_tx_vp8
  video_rx=video_rx_vp8
fi
audio_tx=audio_tx_opus
audio_rx=audio_rx_opus

trap 'sigint' INT
trap 'sigterm' TERM

case "${mod}" in
  -e)
    ${audio_tx} ${audio_tx_port1} 0 1000
    ${video_tx} ${video_tx_port1} 0
    if [ "${nr_stream}" -eq 2 ];
    then
        ${audio_tx} ${audio_tx_port2} 0 1001
        ${video_tx} ${video_tx_port2} 18
    fi
    ;;
  -d)
    ${audio_rx} ${audio_rx_port1}
    ${video_rx} ${video_rx_port1}
    if [ "${nr_stream}" -eq 2 ];
    then
        ${audio_rx} ${audio_rx_port2}
        ${video_rx} ${video_rx_port2}
    fi
    ;;
  *)
    usage
    exit 2
    ;;
esac

sleep 1
echo ""
ps -eao pid,cmd | grep $0 | grep $grep $$
childpid=$(jobs -p)
for pid in ${childpid};
do
  ps -eao pid,cmd | grep ${gst} | grep ${pid}
done
echo ""

wait

echo "exiting"
echo "completed"

exit 0

