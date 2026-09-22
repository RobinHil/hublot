import { useEffect, useState } from "react";

import { Terminal } from "./Terminal";
import { frames } from "./terminal-frames";
import {
  features,
  installs,
  intro,
  keys,
  pillars,
  releases,
  repository,
  tagline,
} from "./content";

export function App() {
  return (
    <div className="min-h-screen bg-ink">
      <Header />
      <main>
        <Hero />
        <Pillars />
        <Features />
        <Keys />
        <Installing />
      </main>
      <Footer />
    </div>
  );
}

function Header() {
  return (
    <header className="sticky top-0 z-10 border-b border-edge/70 bg-ink/85 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center justify-between px-5 py-3">
        <a href="#top" className="flex items-center gap-2.5">
          <Logo className="size-7" />
          <span className="font-mono text-base font-semibold text-mist">
            hublot
          </span>
        </a>
        <nav className="flex items-center gap-5 text-sm text-dim">
          <a className="hover:text-mist" href="#what">
            What it does
          </a>
          <a className="hidden hover:text-mist sm:inline" href="#keys">
            Keys
          </a>
          <a className="hover:text-mist" href="#install">
            Install
          </a>
          <a
            className="rounded-md border border-edge px-3 py-1.5 text-mist hover:border-accent hover:text-accent"
            href={repository}
          >
            GitHub
          </a>
        </nav>
      </div>
    </header>
  );
}

function Hero() {
  return (
    <section id="top" className="border-b border-edge/60">
      <div className="mx-auto max-w-6xl px-5 pt-16 pb-14 sm:pt-24">
        <div className="mx-auto max-w-3xl text-center">
          <Logo className="mx-auto size-20 sm:size-24" />
          <h1 className="mt-7 text-4xl font-semibold tracking-tight text-mist sm:text-6xl">
            {tagline}
          </h1>
          <p className="mx-auto mt-5 max-w-2xl text-lg leading-relaxed text-dim">
            {intro}
          </p>
          <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
            <a
              href="#install"
              className="rounded-lg bg-accent-deep px-5 py-2.5 font-medium text-ink transition hover:bg-accent"
            >
              Install it
            </a>
            <a
              href={releases}
              className="rounded-lg border border-edge px-5 py-2.5 font-medium text-mist transition hover:border-accent hover:text-accent"
            >
              Latest release
            </a>
          </div>
          <p className="mt-4 font-mono text-xs text-dim">
            Linux and macOS, MIT licensed, local daemon only
          </p>
        </div>

        <Terminal
          frame={frames.containers}
          title="hublot: containers"
          className="mt-14"
        />
      </div>
    </section>
  );
}

function Pillars() {
  return (
    <section id="what" className="border-b border-edge/60">
      <div className="mx-auto max-w-6xl space-y-20 px-5 py-20">
        <p className="text-center font-mono text-sm tracking-wide text-accent">
          two things other tools leave out
        </p>

        {pillars.map((pillar) => (
          <article
            key={pillar.title}
            // The frame gets the larger share: it is a fixed grid of
            // characters, so every column it loses is type it has to shrink,
            // while the prose beside it reflows for nothing.
            className="grid gap-10 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] lg:items-center"
          >
            <div>
              <h2 className="text-3xl font-semibold tracking-tight text-mist">
                {pillar.title}
              </h2>
              <p className="mt-4 leading-relaxed text-dim">{pillar.lead}</p>
              <ul className="mt-6 space-y-3">
                {pillar.points.map((point) => (
                  <li key={point} className="flex gap-3 text-sm leading-relaxed text-mist/90">
                    <span aria-hidden className="mt-2 size-1.5 shrink-0 rounded-full bg-accent" />
                    <span>{point}</span>
                  </li>
                ))}
              </ul>
            </div>
            <Terminal frame={frames[pillar.frame]} title={pillar.frameTitle} />
          </article>
        ))}
      </div>
    </section>
  );
}

function Features() {
  return (
    <section className="border-b border-edge/60">
      <div className="mx-auto max-w-6xl px-5 py-20">
        <h2 className="text-center text-3xl font-semibold tracking-tight text-mist">
          And the rest of the job
        </h2>
        <div className="mt-12 grid gap-px overflow-hidden rounded-xl border border-edge bg-edge sm:grid-cols-2 lg:grid-cols-3">
          {features.map((feature) => (
            <div key={feature.title} className="bg-panel p-6">
              <h3 className="font-medium text-mist">{feature.title}</h3>
              <p className="mt-2.5 text-sm leading-relaxed text-dim">{feature.body}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function Keys() {
  return (
    <section id="keys" className="border-b border-edge/60">
      <div className="mx-auto max-w-4xl px-5 py-20">
        <h2 className="text-3xl font-semibold tracking-tight text-mist">
          Lowercase is safe, uppercase is not
        </h2>
        <p className="mt-4 leading-relaxed text-dim">
          One convention, applied everywhere: a lowercase key never destroys
          anything. The help overlay is generated from the same definitions the
          program dispatches on, so it cannot drift from what the keys do.
        </p>
        <dl className="mt-8 grid gap-x-10 gap-y-3 sm:grid-cols-2">
          {keys.map((binding) => (
            <div key={binding.key} className="flex items-baseline gap-4">
              <dt className="w-20 shrink-0 rounded border border-edge bg-panel px-2 py-1 text-center font-mono text-xs text-accent">
                {binding.key}
              </dt>
              <dd className="text-sm text-dim">{binding.description}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}

function Installing() {
  return (
    <section id="install" className="border-b border-edge/60">
      <div className="mx-auto max-w-4xl px-5 py-20">
        <h2 className="text-3xl font-semibold tracking-tight text-mist">
          Install
        </h2>
        <p className="mt-4 leading-relaxed text-dim">
          Native packages, so it uninstalls as cleanly as it installs. Each one
          carries the binary, the man page and a desktop entry that opens it in
          a terminal.
        </p>

        <div className="mt-10 space-y-8">
          {installs.map((install) => (
            <div key={install.id}>
              <h3 className="font-medium text-mist">{install.label}</h3>
              <p className="mt-1 text-sm text-dim">{install.note}</p>
              <CommandBlock commands={install.commands} label={install.label} />
            </div>
          ))}
        </div>

        <p className="mt-10 text-sm leading-relaxed text-dim">
          hublot needs a Docker daemon on the same machine and a user allowed to
          talk to its socket. It says which socket it tried, and why it could
          not be reached, rather than showing a stack trace.
        </p>
      </div>
    </section>
  );
}

/**
 * The commands of one install method, with a button that copies them.
 *
 * What is copied is the commands alone: the `$` in front of each line marks
 * where a command starts, and a shell handed it back reports that no such
 * command exists.
 */
function CommandBlock({ commands, label }: { commands: string[]; label: string }) {
  return (
    <div className="relative mt-3">
      <pre className="overflow-x-auto rounded-lg border border-edge bg-panel py-3 pr-20 pl-4 font-mono text-[12.5px] leading-relaxed text-mist">
        {/* max-content rather than the full width: a line long enough to
            scroll, which the version-resolving one is, otherwise ends flush
            against the right border, because a scroll container drops the
            padding on the side content overflows towards. Sized to its widest
            line, the padding is inside what scrolls and the text keeps its
            margin wherever the block is scrolled to. */}
        <code className="block w-max">
          {commands.map((command) => (
            <div key={command}>
              <span className="mr-2 select-none text-dim">$</span>
              {command}
            </div>
          ))}
        </code>
      </pre>
      {/* A line too long to fit scrolls under the button, and cutting it dead
          against an opaque square reads as a rendering fault. Fading it out
          instead says what is true: the line carries on to the right. */}
      <span
        aria-hidden
        className="pointer-events-none absolute inset-y-px right-px w-20 rounded-r-lg bg-gradient-to-l from-panel via-panel to-transparent"
      />
      <CopyButton text={commands.join("\n")} label={label} />
    </div>
  );
}

/**
 * Copies a block of commands to the clipboard.
 *
 * It appears only once the page has hydrated: the markup is prerendered and
 * meant to read without JavaScript, and a button that cannot do anything is
 * worse than no button at all.
 */
function CopyButton({ text, label }: { text: string; label: string }) {
  const [mounted, setMounted] = useState(false);
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");

  useEffect(() => {
    setMounted(true);
  }, []);

  // The icon says what happened, so it has to go back to saying what the
  // button does. Clearing on a timer, and clearing it again if the button is
  // pressed twice, is what the cleanup is for.
  useEffect(() => {
    if (state === "idle") return;
    const timer = setTimeout(() => setState("idle"), 2000);
    return () => clearTimeout(timer);
  }, [state]);

  if (!mounted) return null;

  // The clipboard is refused outside a secure context and by a browser the
  // user has told to refuse it, and a button that silently does nothing reads
  // as broken, so the refusal is shown.
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setState("copied");
    } catch {
      setState("failed");
    }
  }

  const said = {
    idle: `Copy the ${label} commands`,
    copied: "Copied",
    failed: "Could not copy: select the commands instead",
  }[state];

  return (
    <>
      <button
        type="button"
        onClick={copy}
        title={said}
        aria-label={said}
        className={`absolute top-2 right-2 rounded-md border border-edge bg-panel p-1.5 transition hover:border-accent hover:text-accent ${
          state === "copied"
            ? "text-accent"
            : state === "failed"
              ? "text-danger"
              : "text-dim"
        }`}
      >
        <CopyIcon state={state} />
      </button>
      <span aria-live="polite" className="sr-only">
        {state === "idle" ? "" : said}
      </span>
    </>
  );
}

function CopyIcon({ state }: { state: "idle" | "copied" | "failed" }) {
  const path = {
    idle: "M9 9h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1",
    copied: "m4 12.5 5 5L20 6.5",
    failed: "M6 6l12 12M18 6 6 18",
  }[state];

  return (
    <svg
      aria-hidden
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-4"
    >
      <path d={path} />
    </svg>
  );
}

function Footer() {
  return (
    <footer className="mx-auto max-w-6xl px-5 py-12">
      <div className="flex flex-col gap-6 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <Logo className="size-8" />
          <div>
            <p className="font-mono text-sm text-mist">hublot</p>
            <p className="text-xs text-dim">MIT licensed, by Robin HILAIRE</p>
          </div>
        </div>
        <nav className="flex flex-wrap gap-5 text-sm text-dim">
          <a className="hover:text-mist" href={repository}>
            Source
          </a>
          <a className="hover:text-mist" href={releases}>
            Releases
          </a>
          <a className="hover:text-mist" href={`${repository}/blob/main/AGENTS.md`}>
            How it is built
          </a>
          <a className="hover:text-mist" href={`${repository}/issues`}>
            Issues
          </a>
        </nav>
      </div>
      <p className="mt-8 text-xs leading-relaxed text-dim/80">
        Docker is a trademark of Docker, Inc. hublot is an independent project
        and is not affiliated with, endorsed by, or sponsored by Docker, Inc. It
        speaks to the Docker Engine API and shells out to the Compose CLI, both
        of which are the user's own installation.
      </p>
    </footer>
  );
}

function Logo({ className = "" }: { className?: string }) {
  return (
    <img
      // The base path differs between a project site and a custom domain, so
      // the URL is built from what vite was configured with.
      src={`${import.meta.env.BASE_URL}hublot.svg`}
      alt=""
      width={96}
      height={96}
      className={className}
      aria-hidden
    />
  );
}
