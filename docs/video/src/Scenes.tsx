import React from "react";
import { AbsoluteFill, Easing, Img, interpolate, staticFile, useCurrentFrame } from "remotion";
import data from "./data.json";
import { Backdrop, c, Card, expoOut, mono, Reveal, sans, Scene, StepLabel, typed, useEnter } from "./ui";

const frames = Array.from({ length: 12 }, (_, i) => staticFile(`frames/f${String(i + 1).padStart(2, "0")}.jpg`));
const frameTimes: number[] = data.frameTimes;
// Overview frames closest to the 4 captures the agent attached.
const picked = data.visuals.map((v) => frameTimes.reduce((best, t, i) => (Math.abs(t - v.t) < Math.abs(frameTimes[best] - v.t) ? i : best), 0));

const clamp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;
const avg = (s: Record<string, number>) => Object.values(s).reduce((a, b) => a + b, 0) / Object.values(s).length;

// ---------------------------------------------------------------- 1. Title
export const Title: React.FC<{ duration: number }> = ({ duration }) => {
  const lines = [["Your", "TikToks,"], ["as", "LinkedIn", "posts."]];
  return (
    <Scene duration={duration}>
      <Backdrop />
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", fontFamily: sans }}>
        <Reveal>
          <div style={{ fontFamily: mono, fontSize: 22, color: c.muted, border: `1px solid ${c.border}`, borderRadius: 999, padding: "8px 18px", background: "rgba(255,255,255,0.03)" }}>
            iatomiq-content-repurpose
          </div>
        </Reveal>
        <div style={{ marginTop: 36 }}>
        {lines.map((line, li) => (
        <div key={li} style={{ display: "flex", justifyContent: "center", gap: "0 30px" }}>
          {line.map((w, wi) => (
            <Reveal key={w} delay={8 + (li * 2 + wi) * 4} y={24} blur={14}>
              <span
                style={{
                  fontSize: 132,
                  fontWeight: 600,
                  letterSpacing: "-0.045em",
                  lineHeight: 1.05,
                  backgroundImage: "linear-gradient(180deg, #ffffff 30%, #9ca0a8)",
                  WebkitBackgroundClip: "text",
                  color: "transparent",
                }}
              >
                {w}
              </span>
            </Reveal>
          ))}
        </div>
        ))}
        </div>
        <Reveal delay={34}>
          <div style={{ fontSize: 30, color: c.muted, marginTop: 36, textAlign: "center", lineHeight: 1.45 }}>
            A Claude agent watches the video, writes the post in another author's voice,
            <br />
            and rewrites it until a linter and an editor both pass it.
          </div>
        </Reveal>
      </AbsoluteFill>
    </Scene>
  );
};

// ---------------------------------------------------------------- 2. Input
export const Input: React.FC<{ duration: number }> = ({ duration }) => {
  const frame = useCurrentFrame();
  const cmd = `postwriter -handle ${data.handle} -n 3 -lang en`;
  const typedCmd = typed(cmd, frame, 18, 1.5);
  const doneTyping = 18 + cmd.length / 1.5;
  const logs = [
    `@${data.handle}: repurposing its 3 most-played videos`,
    "style: using cached corpus (8 posts)",
    `[1/3] https://www.tiktok.com/@${data.handle}/video/${data.videoId}`,
  ];
  const phone = useEnter(10);
  const playing = Math.floor(frame / 9) % frames.length;
  return (
    <Scene duration={duration}>
      <Backdrop />
      <StepLabel n="01" title="Point it at a TikTok account" sub="It picks the most-played videos and pulls each transcript." />
      <div style={{ position: "absolute", left: 120, top: 330 }}>
        <Reveal delay={4}>
          <Card style={{ width: 960, height: 420, overflow: "hidden" }}>
            <div style={{ display: "flex", gap: 9, padding: "18px 20px", borderBottom: `1px solid ${c.border}` }}>
              {["#ff5f57", "#febc2e", "#28c840"].map((col) => (
                <div key={col} style={{ width: 13, height: 13, borderRadius: 99, background: col, opacity: 0.85 }} />
              ))}
            </div>
            <div style={{ fontFamily: mono, fontSize: 26, padding: "28px 32px", color: c.text, lineHeight: 1.75 }}>
              <div>
                <span style={{ color: c.green }}>$ </span>
                {typedCmd}
                <span style={{ opacity: frame % 30 < 15 ? 1 : 0, color: c.muted }}>▍</span>
              </div>
              {logs.map((l, i) => (
                <div key={l} style={{ color: c.muted, fontSize: 22, opacity: interpolate(frame, [doneTyping + 6 + i * 8, doneTyping + 14 + i * 8], [0, 1], clamp) }}>
                  {l}
                </div>
              ))}
            </div>
          </Card>
        </Reveal>
      </div>
      <div
        style={{
          position: "absolute",
          right: 170,
          top: 230,
          width: 380,
          height: 676,
          borderRadius: 44,
          border: `1px solid ${c.borderStrong}`,
          padding: 10,
          background: "#000",
          boxShadow: "0 40px 120px rgba(0,0,0,0.7)",
          opacity: phone,
          transform: `translateX(${(1 - phone) * 80}px) rotate(${(1 - phone) * 4}deg)`,
        }}
      >
        <div style={{ position: "relative", width: "100%", height: "100%", borderRadius: 34, overflow: "hidden" }}>
          <Img src={frames[playing]} style={{ width: "100%", height: "100%", objectFit: "cover" }} />
          <div style={{ position: "absolute", inset: 0, background: "linear-gradient(transparent 60%, rgba(0,0,0,0.75))" }} />
          <div style={{ position: "absolute", left: 20, bottom: 26, right: 20, fontFamily: sans, color: "#fff" }}>
            <div style={{ fontWeight: 600, fontSize: 20 }}>@{data.handle}</div>
            <div style={{ fontSize: 16, opacity: 0.85, marginTop: 4 }}>{data.caption}</div>
            <div style={{ fontFamily: mono, fontSize: 14, opacity: 0.7, marginTop: 8 }}>{Math.round(data.plays / 1000)}K plays</div>
          </div>
        </div>
      </div>
    </Scene>
  );
};

// ---------------------------------------------------------------- 3. Watch
export const Watch: React.FC<{ duration: number }> = ({ duration }) => {
  const frame = useCurrentFrame();
  const W = 118, H = 210, GAP = 16;
  const total = frames.length * W + (frames.length - 1) * GAP;
  const left0 = (1920 - total) / 2;
  const scanStart = 22, scanEnd = 120;
  const scanX = interpolate(frame, [scanStart, scanEnd], [0, total], { ...clamp, easing: expoOut });
  const selectAt = 128;
  const sel = useEnter(selectAt, 18);
  return (
    <Scene duration={duration}>
      <Backdrop />
      <StepLabel n="02" title="The agent watches the video" sub="It looks at any second it wants, then picks the frames worth attaching." />
      <div style={{ position: "absolute", left: left0, top: 470, width: total, height: H }}>
        {frames.map((src, i) => {
          const x = i * (W + GAP);
          const enter = interpolate(frame, [4 + i * 2, 20 + i * 2], [0, 1], { ...clamp, easing: expoOut });
          const isPicked = picked.includes(i);
          const passed = scanX > x + W / 2;
          const lift = isPicked ? sel : 0;
          const dim = isPicked ? 1 : interpolate(sel, [0, 1], [1, 0.3]);
          return (
            <div
              key={src}
              style={{
                position: "absolute",
                left: x,
                top: 0,
                width: W,
                height: H,
                borderRadius: 12,
                overflow: "hidden",
                opacity: enter * dim,
                transform: `translateY(${(1 - enter) * 30 - lift * 34}px) scale(${1 + lift * 0.12})`,
                outline: isPicked && passed ? `2px solid rgba(255,255,255,${0.35 + lift * 0.65})` : `1px solid ${c.border}`,
                outlineOffset: 3,
                boxShadow: isPicked ? `0 20px 60px rgba(0,0,0,${0.3 + lift * 0.4})` : "none",
              }}
            >
              <Img src={src} style={{ width: "100%", height: "100%", objectFit: "cover" }} />
              {isPicked && (
                <div
                  style={{
                    position: "absolute",
                    top: 8,
                    left: 8,
                    width: 30,
                    height: 30,
                    borderRadius: 99,
                    background: c.text,
                    color: c.bg,
                    fontFamily: mono,
                    fontSize: 16,
                    fontWeight: 600,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    opacity: sel,
                    transform: `scale(${sel})`,
                  }}
                >
                  {picked.indexOf(i) + 1}
                </div>
              )}
            </div>
          );
        })}
        {/* scan line */}
        <div
          style={{
            position: "absolute",
            left: scanX - 1,
            top: -30,
            width: 2,
            height: H + 60,
            background: "linear-gradient(transparent, #fff, transparent)",
            boxShadow: "0 0 24px 4px rgba(255,255,255,0.35)",
            opacity: interpolate(frame, [scanStart, scanStart + 6, scanEnd - 4, scanEnd + 4], [0, 1, 1, 0], clamp),
          }}
        />
        {/* view_frame calls, as the scan passes */}
        {data.visuals.map((v, k) => {
          const i = picked[k];
          const x = i * (W + GAP);
          const t0 = interpolate(x + W / 2, [0, total], [scanStart, scanEnd]);
          const p = interpolate(frame, [t0, t0 + 10], [0, 1], { ...clamp, easing: expoOut });
          return (
            <div
              key={v.t}
              style={{
                position: "absolute",
                left: x + W / 2 - 90,
                top: H + 44 + (k % 2) * 46,
                width: 180,
                textAlign: "center",
                fontFamily: mono,
                fontSize: 17,
                color: c.muted,
                opacity: p,
                transform: `translateY(${(1 - p) * 10}px)`,
              }}
            >
              <span style={{ border: `1px solid ${c.border}`, background: c.panel, borderRadius: 8, padding: "5px 10px" }}>
                view_frame <span style={{ color: c.text }}>{v.t}s</span>
              </span>
            </div>
          );
        })}
      </div>
      <Reveal delay={selectAt + 10} style={{ position: "absolute", left: 0, right: 0, top: 860, textAlign: "center" }}>
        <div style={{ fontFamily: sans, fontSize: 28, color: c.muted }}>
          <span style={{ color: c.text }}>set_visuals</span> · the command typed in ChatGPT, then what it produced
        </div>
      </Reveal>
    </Scene>
  );
};

// ---------------------------------------------------------------- 4. Review loop
const criteria = ["hook", "value", "fidelity", "voice", "human", "close", "language"] as const;

export const Review: React.FC<{ duration: number }> = ({ duration }) => {
  const frame = useCurrentFrame();
  const states = [...data.rounds, data.final];
  const per = 34, start = 26;
  const idx = Math.min(states.length - 1, Math.max(0, Math.floor((frame - start) / per)));
  const prev = states[Math.max(0, idx - 1)] as Record<string, number>;
  const cur = states[idx] as Record<string, number>;
  const local = interpolate(frame - start - idx * per, [0, 14], [0, 1], { ...clamp, easing: expoOut });
  const grow = interpolate(frame, [start - 14, start + 6], [0, 1], { ...clamp, easing: expoOut });
  const score = (k: string) => (idx === 0 ? cur[k] * grow : prev[k] + (cur[k] - prev[k]) * local);
  const average = criteria.reduce((a, k) => a + score(k), 0) / criteria.length;
  const isFinal = idx === states.length - 1 && frame > start + idx * per;
  const ship = useEnter(start + (states.length - 1) * per + 8, 14);
  const label = isFinal ? "Final" : `Round ${idx + 1} of ${data.rounds.length}`;
  const postLines = data.post.split("\n");
  const valeChecks = ["No em dashes or AI filler", "No mention of TikTok or the handle", `Hook ${data.lint.hook_chars} chars, under the fold`, `${data.lint.chars} / 3000 characters`];
  const fix = data.firstFix.split(". ")[0] + ".";

  return (
    <Scene duration={duration}>
      <Backdrop />
      <StepLabel n="03" title="Write. Lint. Review. Repeat." sub="Vale checks the prose. A separate Claude editor scores it. The writer revises." />
      {/* draft */}
      <div style={{ position: "absolute", left: 120, top: 300 }}>
        <Reveal delay={4}>
          <Card style={{ width: 760, height: 640, padding: 34, overflow: "hidden", position: "relative" }}>
            <div style={{ fontFamily: mono, fontSize: 16, color: c.faint, marginBottom: 18 }}>draft.en.md</div>
            <div style={{ fontFamily: sans, fontSize: 19, lineHeight: 1.5, color: "#c9ccd1", whiteSpace: "pre-wrap" }}>
              {postLines.slice(0, 9).join("\n")}
            </div>
            <div style={{ position: "absolute", left: 0, right: 0, bottom: 0, height: 300, background: `linear-gradient(transparent, ${c.panel} 40%)` }} />
            <div style={{ position: "absolute", left: 34, right: 34, bottom: 30 }}>
              <div style={{ fontFamily: mono, fontSize: 15, color: c.faint, marginBottom: 12 }}>vale --config vale/.vale.ini</div>
              {valeChecks.map((t, i) => (
                <Reveal key={t} delay={20 + i * 5} y={8} blur={4}>
                  <div style={{ display: "flex", gap: 12, alignItems: "center", fontFamily: sans, fontSize: 20, color: c.text, marginTop: 8 }}>
                    <span style={{ color: c.green, fontWeight: 600 }}>✓</span>
                    {t}
                  </div>
                </Reveal>
              ))}
            </div>
          </Card>
        </Reveal>
      </div>
      {/* editor */}
      <div style={{ position: "absolute", left: 930, top: 300 }}>
        <Reveal delay={10}>
          <Card style={{ width: 870, height: 640, padding: 34, position: "relative" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <div style={{ fontFamily: mono, fontSize: 16, color: c.faint }}>review_post · editor</div>
              <div style={{ fontFamily: mono, fontSize: 18, color: c.muted, border: `1px solid ${c.border}`, borderRadius: 999, padding: "4px 14px" }}>{label}</div>
            </div>
            <div style={{ marginTop: 26 }}>
              {criteria.map((k) => {
                const v = score(k);
                return (
                  <div key={k} style={{ display: "flex", alignItems: "center", gap: 18, height: 44, fontFamily: sans }}>
                    <div style={{ width: 110, fontSize: 20, color: c.muted }}>{k}</div>
                    <div style={{ flex: 1, height: 8, borderRadius: 99, background: "rgba(255,255,255,0.06)", position: "relative" }}>
                      <div style={{ position: "absolute", inset: 0, width: `${v * 10}%`, borderRadius: 99, background: v >= 7 ? c.text : c.amber }} />
                    </div>
                    <div style={{ width: 34, textAlign: "right", fontFamily: mono, fontSize: 20, color: c.text }}>{Math.round(v)}</div>
                  </div>
                );
              })}
            </div>
            <div style={{ display: "flex", alignItems: "baseline", gap: 16, marginTop: 22, fontFamily: sans }}>
              <span style={{ fontSize: 64, fontWeight: 600, letterSpacing: "-0.03em", color: c.text, fontVariantNumeric: "tabular-nums" }}>{average.toFixed(1)}</span>
              <span style={{ fontSize: 22, color: c.muted }}>average · ships at 7.5</span>
              <div
                style={{
                  marginLeft: "auto",
                  alignSelf: "center",
                  opacity: ship,
                  transform: `scale(${0.8 + ship * 0.2})`,
                  background: "rgba(62,207,142,0.12)",
                  border: "1px solid rgba(62,207,142,0.4)",
                  color: c.green,
                  borderRadius: 999,
                  padding: "8px 20px",
                  fontSize: 24,
                  fontWeight: 600,
                }}
              >
                ✓ Ships
              </div>
            </div>
            <div
              style={{
                position: "absolute",
                left: 34,
                right: 34,
                bottom: 30,
                fontFamily: sans,
                fontSize: 19,
                color: c.muted,
                borderLeft: `2px solid ${c.borderStrong}`,
                paddingLeft: 16,
                opacity: interpolate(frame, [start + 4, start + 14, start + 3 * per, start + 3 * per + 10], [0, 1, 1, 0], clamp),
              }}
            >
              <span style={{ fontFamily: mono, fontSize: 15, color: c.faint }}>round 1 fix · </span>
              {fix}
            </div>
          </Card>
        </Reveal>
      </div>
    </Scene>
  );
};

// ---------------------------------------------------------------- 5. Output
export const Output: React.FC<{ duration: number }> = ({ duration }) => {
  const frame = useCurrentFrame();
  const card = useEnter(4);
  const scroll = interpolate(frame, [60, duration - 25], [0, -1080], { ...clamp, easing: Easing.inOut(Easing.cubic) });
  const chips = ["0 Vale errors", "No TikTok mention", `${data.lint.chars} characters`, "4 frames attached"];
  return (
    <Scene duration={duration}>
      <Backdrop />
      <StepLabel n="04" title="Ready to post" sub="Published by the creator, as their own LinkedIn post." />
      <div
        style={{
          position: "absolute",
          left: 120,
          top: 300,
          width: 820,
          height: 700,
          overflow: "hidden",
          borderRadius: 16,
          opacity: card,
          transform: `translateY(${(1 - card) * 60}px)`,
          maskImage: "linear-gradient(black 85%, transparent)",
        }}
      >
        <div style={{ transform: `translateY(${scroll}px)`, background: "#1b1f23", border: "1px solid #38434f", borderRadius: 16, padding: 30, fontFamily: sans }}>
          <div style={{ display: "flex", gap: 16, alignItems: "center" }}>
            <Img src={frames[1]} style={{ width: 64, height: 64, borderRadius: 99, objectFit: "cover", objectPosition: "50% 78%" }} />
            <div>
              <div style={{ color: "#e9e9e9", fontSize: 22, fontWeight: 600 }}>{data.handle}</div>
              <div style={{ color: "#a0a7ad", fontSize: 17 }}>AI tips you can copy · 1m</div>
            </div>
            <div style={{ marginLeft: "auto", color: "#71b7fb", fontSize: 20, fontWeight: 600 }}>+ Follow</div>
          </div>
          <div style={{ color: "#e9e9e9", fontSize: 21, lineHeight: 1.45, whiteSpace: "pre-wrap", marginTop: 22 }}>{data.post}</div>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 4, marginTop: 22, borderRadius: 8, overflow: "hidden" }}>
            {[1, 2, 3, 4].map((i) => (
              <Img key={i} src={staticFile(`visual-${i}.jpg`)} style={{ width: "100%", height: 340, objectFit: "cover", objectPosition: "50% 20%" }} />
            ))}
          </div>
        </div>
      </div>
      <div style={{ position: "absolute", left: 1040, top: 360 }}>
        {chips.map((t, i) => (
          <Reveal key={t} delay={30 + i * 7} y={14}>
            <div
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 14,
                marginBottom: 22,
                fontFamily: sans,
                fontSize: 30,
                color: c.text,
                border: `1px solid ${c.border}`,
                background: "rgba(255,255,255,0.03)",
                borderRadius: 14,
                padding: "16px 24px",
              }}
            >
              <span style={{ color: c.green }}>✓</span>
              {t}
            </div>
            <br />
          </Reveal>
        ))}
        <Reveal delay={62}>
          <div style={{ fontFamily: mono, fontSize: 20, color: c.faint, marginTop: 14, lineHeight: 1.6 }}>
            posts/{data.handle}/{data.videoId}-en-auto/
            <br />
            ├ post.txt
            <br />
            ├ visual-1.jpg … visual-4.jpg
            <br />
            └ meta.json
          </div>
        </Reveal>
      </div>
    </Scene>
  );
};

// ---------------------------------------------------------------- 6. Outro
export const Outro: React.FC<{ duration: number }> = ({ duration }) => (
  <Scene duration={duration + 12}>
    <Backdrop />
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", fontFamily: sans }}>
      <Reveal y={20} blur={12}>
        <div style={{ fontSize: 88, fontWeight: 600, letterSpacing: "-0.04em", color: c.text }}>iatomiq-content-repurpose</div>
      </Reveal>
      <Reveal delay={8}>
        <div style={{ fontSize: 30, color: c.muted, marginTop: 20 }}>Go · Claude Opus 5.5 · Vale · ffmpeg · ScrapeCreators</div>
      </Reveal>
      <Reveal delay={16}>
        <div style={{ fontFamily: mono, fontSize: 26, color: c.text, marginTop: 48, border: `1px solid ${c.borderStrong}`, borderRadius: 12, padding: "14px 26px", background: "rgba(255,255,255,0.03)" }}>
          github.com/alexisbcz/iatomiq-content-repurpose
        </div>
      </Reveal>
      <Reveal delay={22}>
        <div style={{ fontSize: 22, color: c.faint, marginTop: 22 }}>MIT licensed</div>
      </Reveal>
    </AbsoluteFill>
  </Scene>
);

export { avg };
