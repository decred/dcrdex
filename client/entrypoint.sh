#!/bin/sh

# Get the IP address of the primary network interface
IP_ADDRESS=$(hostname -I | awk '{print $1}')

CMD="./bisonw"
if [ "$IP_ADDRESS" != "" ]; then
    CMD="$CMD --webaddr $IP_ADDRESS:5758"
fi

exec $CMD
