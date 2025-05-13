#!/bin/bash

# Save the arguments into variables.
system="${1}"
rom="${2}"
romname="${3}"

# Convert an argument into another value.
if [[ "${system}" == "fbneo" ]]; then
        system="mame"
fi

# Switch case for certain systems.
case ${system} in
  fbneo)
    system="mame"
    ;;
  scummvm)
    rom="${rom%.*}"
    ;;
esac

# Execute this part every time this event triggers.
echo ${rom} > /var/run/zaparoo.tmp