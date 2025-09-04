#!/bin/sh
BASE="$HOME/firecrawl/apps/api"

tmux new-session -d -s fc -n main
tmux split-window -v -t "fc:main" 

tmux send-keys -t "fc:main.0" "cd $BASE; PORT=3001 pnpm run workers" C-m 
tmux send-keys -t "fc:main.1" "cd $BASE; PORT=3002 pnpm run start" C-m

tmux attach-session -t fc
