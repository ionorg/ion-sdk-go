#!/bin/bash

# default values
sn='ion'
nr_client=-1
nr_stream=-1
program=${PWD}/ion-sfu-rtp
pidfile=/tmp/ion_sfu_rtp_run_${USER}.pid

usage()
{
  echo
  echo "$0 <session name> <nr_client> <nr_stream>"
  echo "$0 stop - to stop all the clients"
  echo ""
  echo "session name = any valid session name"
  echo "nr_client    = 1 or above, upto whatever the system could handle"
  echo "nr_stream    = either 1 or 2 only"
  echo ""
  echo "For example:"
  echo "to start 5 clients with 2 streams per client with session name ion"
  echo
  echo "$0 ion 5 2"
  echo
}

stop_clients()
{
  echo stopping
  childpid=$(pidof ${program})
  nr=0
  for pid in ${childpid};
  do
    ps -eao pid,cmd | grep ${program} | grep ${pid}
    let nr=${nr}+1
  done
  echo "Number of processes: ${nr}"
  sleep 1
  echo killall -15 ${program}
  for pid in ${childpid};
  do
    kill -15 ${pid}
  done
  sleep 1
  echo killall -9 ${program}
  killall -9 ${program}
  echo "exiting"
  echo "completed"
}

if [ "$#" -lt 1 ];
then
  usage
  exit 2
fi

sn=${1}
if [ "$sn" == "stop" ];
then
  stop_clients
  exit 0
fi

if [ "$#" -ne 3 ];
then
  usage
  exit 2
fi

nr_client=${2}
nr_stream=${3}
args="-addr localhost:5551 -session ${sn} -nr_stream ${nr_stream}"

if [ ! -f ${program} ];
then
  echo "${program} not found"
  go build .
  exit 2
fi

echo "Session Name: ${sn}"
echo "NR Client: ${nr_client}"
echo "NR Stream: ${nr_stream}"
echo "${program} ${args} -client_id cl000"
echo

for((n=0;n<${nr_client};n++));
do
  sleep 2
  client_id=$(printf "cl%03d" ${n})
  echo ${n} ${client_id}
  echo setsid -f ${program} ${args} -client_id ${client_id}
  setsid -f ${program} ${args} -client_id ${client_id}
done

sleep 5
echo "started"
rm -f ${pidfile}
childpid=$(pidof ${program})
echo ${childpid} > ${pidfile}
for pid in ${childpid};
do
  ps -eao pid,cmd | grep ${program} | grep ${pid}
done
echo cat ${pidfile}
cat ${pidfile}

exit 0

