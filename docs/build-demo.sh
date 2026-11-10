#!/usr/bin/env bash
# Rebuilds docs/demo.mp4 and docs/demo.gif from a real run.
# Needs vhs, ffmpeg and ImageMagick. Costs one postwriter run (ScrapeCreators credits + Claude calls).
set -euo pipefail
cd "$(dirname "$0")/.."

vhs docs/demo.tape  # writes docs/terminal.mp4 and posts/iatomiq/<id>-en-auto-demo/

post=$(ls -d posts/iatomiq/*-en-auto-demo | head -1)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
bg='#1e1e2e'; fg='#cdd6f4'; sub='#a6adc8'

magick -size 1280x720 xc:$bg -font /System/Library/Fonts/HelveticaNeue.ttc -fill $fg -pointsize 54 -gravity center \
  -annotate +0-60 'iatomiq-content-repurpose' -fill $sub -pointsize 26 \
  -annotate +0+20 'TikTok video in, LinkedIn post out.' \
  -annotate +0+60 'A Claude agent writes it, Vale and an editor check it.' "$tmp/title.png"

for f in "$post"/visual-*.jpg; do magick "$f" -resize x330 "$tmp/$(basename "$f" .jpg).png"; done
v=( $(ls "$tmp"/visual-*.png | head -4) )
if [ "${#v[@]}" -ge 4 ]; then
  magick \( "${v[0]}" "${v[1]}" +append \) \( "${v[2]}" "${v[3]}" +append \) -background $bg -splice 0x10 -append -resize x670 "$tmp/grid.png"
else
  magick "${v[@]}" +append -resize x670 "$tmp/grid.png"
fi
magick -size 640x670 -background $bg -fill $fg -font /System/Library/Fonts/Menlo.ttc -pointsize 13.5 caption:@"$post/post.txt" "$tmp/text.png"
magick -size 1280x720 xc:$bg "$tmp/text.png" -geometry +30+25 -composite "$tmp/grid.png" -gravity northeast -geometry +30+25 -composite "$tmp/result.png"

# Title, then the terminal (the agent's working time sped up 5x), then the result.
dur=$(ffprobe -v error -show_entries format=duration -of csv=p=0 docs/terminal.mp4)
end=$(echo "$dur - 7" | bc)
ffmpeg -loglevel error -y -loop 1 -t 3.5 -i "$tmp/title.png" -loop 1 -t 9 -i "$tmp/result.png" -i docs/terminal.mp4 -filter_complex "\
[0:v]fps=30,scale=1280:720,format=yuv420p,setsar=1[t];\
[2:v]fps=30,scale=1280:720,format=yuv420p,setsar=1,split=3[a][b][c];\
[a]trim=0:12,setpts=PTS-STARTPTS[t1];\
[b]trim=12:$end,setpts=(PTS-STARTPTS)/5[t2];\
[c]trim=$end,setpts=PTS-STARTPTS,tpad=stop_mode=clone:stop_duration=3[t3];\
[1:v]fps=30,scale=1280:720,format=yuv420p,setsar=1[r];\
[t][t1][t2][t3][r]concat=n=5:v=1:a=0[out]" -map "[out]" -c:v libx264 -crf 23 -preset slow -movflags +faststart docs/demo.mp4
ffmpeg -loglevel error -y -i docs/demo.mp4 -vf "fps=8,scale=960:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=96[p];[s1][p]paletteuse=dither=bayer:bayer_scale=4" docs/demo.gif
echo "docs/demo.mp4 and docs/demo.gif rebuilt from $post"
