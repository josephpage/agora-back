#!/bin/bash
# Same interface as the Kotlin app's launchCustomCommand.sh.
if [[ $# -ge 1 ]]
then
  echo "Launch command: bin/agora --run-custom-command=$@"
  bin/agora --run-custom-command=$@
else
  echo "This script needs at least 1 argument !"
fi
