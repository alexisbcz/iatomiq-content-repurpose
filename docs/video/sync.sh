#!/usr/bin/env bash
# Pulls a real postwriter run into the video project: post, scores, attached frames,
# and 12 frames of the source video. Then: npm run render && npm run gif
#
#   ../../bin/postwriter -video <url> -lang en -tag demo -overview 12 \
#     -visual-hint "frames where the prompt is typed in ChatGPT, paired with the result"
#   ./sync.sh ../../posts/iatomiq/<video-id>-en-auto-demo
#
# Needs SCRAPECREATORS_API_KEY (read from ../../.env), ffmpeg and python3. Costs 1 credit.
set -euo pipefail
cd "$(dirname "$0")"
post=${1:?usage: ./sync.sh <post folder>}
set -a; source ../../.env; set +a
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

url=$(python3 -I -c "import json,sys; print(json.load(open(sys.argv[1]))['source_video'])" "$post/meta.json")
curl -sf "https://api.scrapecreators.com/v2/tiktok/video?url=$url&trim=true" -H "x-api-key: $SCRAPECREATORS_API_KEY" > "$tmp/video.json"
mp4=$(python3 -I -c "import json,sys; v=json.load(open(sys.argv[1]))['aweme_detail']['video']; print((v['download_no_watermark_addr']['url_list']+v['play_addr']['url_list'])[0])" "$tmp/video.json")
curl -sfL -A "Mozilla/5.0" -e https://www.tiktok.com/ "$mp4" -o "$tmp/video.mp4"

mkdir -p public/frames
cp "$post"/visual-*.jpg public/
python3 -I - "$post" "$tmp/video.json" <<'PY'
import json, sys
post, video = sys.argv[1], sys.argv[2]
m = json.load(open(post + "/meta.json"))
a = json.load(open(video))["aweme_detail"]
dur = a["video"]["duration"] / 1000
data = {
    "post": open(post + "/post.txt").read().strip(),
    "rounds": [r["editor"]["scores"] for r in m["review_rounds"]],
    "final": m["final_editor"]["scores"],
    "firstFix": m["review_rounds"][0]["editor"]["fixes"][0],
    "lint": {k: m["final_lint"][k] for k in ("errors", "warnings", "chars", "hook_chars")},
    "visuals": [{"t": v["seconds"], "why": v["why"]} for v in m["visuals"]],
    "caption": a["desc"].split("\n")[0][:90],
    "plays": a["statistics"]["play_count"],
    "handle": a["author"]["unique_id"],
    "videoId": a["aweme_id"],
    # 12 evenly spaced frames, snapped to the attached captures so the film strip shows them.
    "frameTimes": sorted({round(dur * (i + 0.5) / 12, 1) for i in range(12)}),
}
for v in data["visuals"]:
    i = min(range(len(data["frameTimes"])), key=lambda k: abs(data["frameTimes"][k] - v["t"]))
    data["frameTimes"][i] = v["t"]
json.dump(data, open("src/data.json", "w"), indent=1, ensure_ascii=False)
open("frame-times.txt", "w").write("\n".join(map(str, data["frameTimes"])))
PY
i=0
while read -r t; do
  i=$((i + 1))
  ffmpeg -loglevel error -y -ss "$t" -i "$tmp/video.mp4" -frames:v 1 -vf "scale=-2:640" -q:v 4 "public/frames/f$(printf %02d $i).jpg"
done < frame-times.txt
rm frame-times.txt
echo "synced $post"
