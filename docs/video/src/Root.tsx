import React from "react";
import { Composition, Series } from "remotion";
import { Input, Output, Outro, Review, Title, Watch } from "./Scenes";

// Scene lengths in frames (30 fps).
const scenes = [
  { C: Title, d: 105 },
  { C: Input, d: 150 },
  { C: Watch, d: 200 },
  { C: Review, d: 250 },
  { C: Output, d: 230 },
  { C: Outro, d: 100 },
];

const Demo: React.FC = () => (
  <Series>
    {scenes.map(({ C, d }, i) => (
      <Series.Sequence key={i} durationInFrames={d}>
        <C duration={d} />
      </Series.Sequence>
    ))}
  </Series>
);

export const Root: React.FC = () => (
  <Composition
    id="Demo"
    component={Demo}
    durationInFrames={scenes.reduce((a, s) => a + s.d, 0)}
    fps={30}
    width={1920}
    height={1080}
  />
);
