import React from "react";
import {
  AbsoluteFill,
  Easing,
  interpolate,
  spring,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { loadFont as loadGeist } from "@remotion/google-fonts/Geist";
import { loadFont as loadGeistMono } from "@remotion/google-fonts/GeistMono";

export const sans = loadGeist("normal", { weights: ["400", "500", "600"] }).fontFamily;
export const mono = loadGeistMono("normal", { weights: ["400", "500"] }).fontFamily;

export const c = {
  bg: "#08090a",
  panel: "#101113",
  panel2: "#16171a",
  border: "rgba(255,255,255,0.08)",
  borderStrong: "rgba(255,255,255,0.14)",
  text: "#f7f8f8",
  muted: "#8a8f98",
  faint: "#62666d",
  green: "#3ecf8e",
  amber: "#f5a524",
};

export const expoOut = Easing.bezier(0.16, 1, 0.3, 1);

/** 0 → 1 with a soft spring, starting at `delay` frames. */
export const useEnter = (delay = 0, damping = 200) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  return spring({ frame: frame - delay, fps, config: { damping } });
};

/** Fades, lifts and un-blurs its children in. */
export const Reveal: React.FC<{
  delay?: number;
  y?: number;
  blur?: number;
  style?: React.CSSProperties;
  children: React.ReactNode;
}> = ({ delay = 0, y = 16, blur = 8, style, children }) => {
  const p = useEnter(delay);
  return (
    <div
      style={{
        opacity: p,
        transform: `translateY(${(1 - p) * y}px)`,
        filter: `blur(${(1 - p) * blur}px)`,
        ...style,
      }}
    >
      {children}
    </div>
  );
};

/** Background shared by every scene: near-black, faint grid, a soft top light. */
export const Backdrop: React.FC = () => (
  <AbsoluteFill style={{ backgroundColor: c.bg }}>
    <AbsoluteFill
      style={{
        backgroundImage:
          "linear-gradient(rgba(255,255,255,0.035) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.035) 1px, transparent 1px)",
        backgroundSize: "64px 64px",
        maskImage: "radial-gradient(ellipse 70% 60% at 50% 40%, black, transparent 75%)",
      }}
    />
    <AbsoluteFill
      style={{
        background: "radial-gradient(ellipse 60% 45% at 50% -10%, rgba(255,255,255,0.09), transparent 70%)",
      }}
    />
  </AbsoluteFill>
);

/** Wraps a scene: blurs in at the start, out at the end. */
export const Scene: React.FC<{ duration: number; children: React.ReactNode }> = ({ duration, children }) => {
  const frame = useCurrentFrame();
  const out = interpolate(frame, [duration - 12, duration], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.in(Easing.cubic),
  });
  return (
    <AbsoluteFill style={{ opacity: 1 - out, filter: `blur(${out * 10}px)`, transform: `scale(${1 - out * 0.02})` }}>
      {children}
    </AbsoluteFill>
  );
};

/** "01  Point it at an account" in the top-left corner. */
export const StepLabel: React.FC<{ n: string; title: string; sub?: string }> = ({ n, title, sub }) => (
  <div style={{ position: "absolute", left: 120, top: 96, fontFamily: sans }}>
    <Reveal>
      <div style={{ display: "flex", alignItems: "baseline", gap: 20 }}>
        <span style={{ fontFamily: mono, fontSize: 22, color: c.faint }}>{n}</span>
        <span style={{ fontSize: 44, fontWeight: 600, letterSpacing: "-0.03em", color: c.text }}>{title}</span>
      </div>
    </Reveal>
    {sub && (
      <Reveal delay={6}>
        <div style={{ fontSize: 24, color: c.muted, marginTop: 10, marginLeft: 46 }}>{sub}</div>
      </Reveal>
    )}
  </div>
);

export const Card: React.FC<{ style?: React.CSSProperties; children: React.ReactNode }> = ({ style, children }) => (
  <div
    style={{
      background: `linear-gradient(180deg, ${c.panel2}, ${c.panel})`,
      border: `1px solid ${c.border}`,
      borderRadius: 16,
      boxShadow: "0 1px 0 rgba(255,255,255,0.05) inset, 0 30px 80px rgba(0,0,0,0.5)",
      ...style,
    }}
  >
    {children}
  </div>
);

export const typed = (text: string, frame: number, start: number, cps = 1.4) =>
  text.slice(0, Math.max(0, Math.floor((frame - start) * cps)));
