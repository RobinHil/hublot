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
              <pre className="mt-3 overflow-x-auto rounded-lg border border-edge bg-panel px-4 py-3 font-mono text-[12.5px] leading-relaxed text-mist">
                <code>
                  {install.commands.map((command) => (
                    <div key={command}>
                      <span className="mr-2 select-none text-dim">$</span>
                      {command}
                    </div>
                  ))}
                </code>
              </pre>
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
