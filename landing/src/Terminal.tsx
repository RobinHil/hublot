import type React from "react";

import type { Frame } from "./terminal-frames";

/**
 * How wide the capture was. Frames are taken at the width they are shown at, so
 * this is read from the frame rather than assumed: every row is then padded to
 * it, which is what makes the reverse-video cursor row span the full line the
 * way it does on screen.
 */
function columnsOf(frame: Frame): number {
  return frame.reduce(
    (widest, row) => Math.max(widest, row.reduce((n, run) => n + run.t.length, 0)),
    0,
  );
}

type Props = {
  frame: Frame;
  /** Shown in the window chrome, as a terminal title would be. */
  title: string;
  className?: string;
};

/**
 * A real hublot frame, rendered as text rather than as a picture: it stays
 * sharp at any zoom, it can be selected and read by a screen reader, and it
 * weighs a few kilobytes.
 */
export function Terminal({ frame, title, className = "" }: Props) {
  const columns = columnsOf(frame);
  const plain = frame
    .map((row) => row.map((run) => run.t).join(""))
    .join("\n")
    .trimEnd();

  return (
    <figure
      className={`overflow-hidden rounded-xl border border-edge bg-ink shadow-2xl shadow-black/40 ${className}`}
    >
      <div className="flex items-center gap-2 border-b border-edge bg-panel px-4 py-2.5">
        <span className="size-3 rounded-full bg-danger/70" />
        <span className="size-3 rounded-full bg-warn/70" />
        <span className="size-3 rounded-full bg-accent/50" />
        <span className="ml-2 font-mono text-xs text-dim">{title}</span>
      </div>

      {/* The frame is a fixed grid of characters, so rather than let it scroll
          out of view the type is sized to whatever width it is given: the whole
          capture is always visible, on a phone as on a wide screen. */}
      <div
        className="terminal-fit px-3 py-3"
        style={{ "--cols": columns } as React.CSSProperties}
      >
        <pre
          className="font-mono leading-[1.5]"
          aria-label={`Screenshot: ${title}`}
        >
          <code>
            {frame.map((row, y) => (
              <Row key={y} row={row} columns={columns} />
            ))}
          </code>
        </pre>
      </div>

      {/* The same content as text, for anything that cannot read the runs. */}
      <figcaption className="sr-only">{plain}</figcaption>
    </figure>
  );
}

function Row({ row, columns }: { row: Frame[number]; columns: number }) {
  const used = row.reduce((n, run) => n + run.t.length, 0);
  const padding = Math.max(0, columns - used);

  return (
    <div>
      {row.map((run, i) => {
        // Reverse video is how the table paints the selected row: swapping the
        // colours here keeps that readable instead of inventing a highlight.
        const style = run.r
          ? { backgroundColor: run.c ?? "#1f3a5f", color: "#0b0f14" }
          : { color: run.c };

        return (
          <span
            key={i}
            style={{ ...style, fontWeight: run.b ? 600 : undefined }}
          >
            {run.t}
          </span>
        );
      })}
      {padding > 0 ? " ".repeat(padding) : ""}
      {"\n"}
    </div>
  );
}
