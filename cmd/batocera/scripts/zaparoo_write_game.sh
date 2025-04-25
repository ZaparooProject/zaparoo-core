#!/bin/bash

# Retrieve latest selected item
item="$(curl -sSL http://127.0.0.1:7497/selected-item)"

# POST to batocera to put a card on the reader
curl -X POST --data "Place a tag on the NFC writer to write '${item}'. Write will timeout in 30 seconds" http://127.0.0.1:1234/messagebox

/userdata/system/zaparoo -write "${item}"

if [ $? -eq 0 ]; then
    curl -X POST --data "The tag '${item}' has been written sucesfully!" http://127.0.0.1:1234/messagebox
else
     curl -X POST --data "Something went wrong writing the token :(" http://127.0.0.1:1234/messagebox
fi