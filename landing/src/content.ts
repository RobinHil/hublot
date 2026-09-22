/** Everything the page says, kept out of the markup. */

export const repository = "https://github.com/RobinHil/hublot";
export const releases = `${repository}/releases/latest`;

export const tagline = "Your Docker host, whole, in one terminal window.";

export const intro =
  "Live resource usage at a glance, then keyboard-driven navigation into every object the daemon manages, with every action available without leaving the terminal.";

export type Pillar = {
  title: string;
  lead: string;
  points: string[];
  frame: "disk" | "compose";
  frameTitle: string;
};

export const pillars: Pillar[] = [
  {
    title: "See what a prune would destroy",
    lead: "The Docker API has no dry run: prune endpoints delete first and report afterwards. hublot reconstructs the preview, applying the same filters the daemon would, and lists the objects by name before anything is removed.",
    points: [
      "Every category pruned separately, or all of them at once",
      "Objects belonging to a Compose project shown first and flagged, because a blanket prune is how stacks lose their data",
      "Volume rules follow the negotiated API version, which changed what a default prune takes",
      "The confirmation states what goes, never just asks whether you are sure",
    ],
    frame: "disk",
    frameTitle: "hublot: disk",
  },
  {
    title: "Compose stacks, not loose containers",
    lead: "Compose is a CLI plugin that leaves labels on ordinary objects. hublot reads those labels back into projects and services, and tells you whether what is running still matches the YAML on disk.",
    points: [
      "Drift detection per service, from the same config hash Compose itself compares",
      "Each project is a line of its own: act on it for the whole stack, on a service line for that service",
      "Stopped stacks stay visible through their volumes and networks, where wasted disk hides",
      "Read the compose file beside the stack, or open it in your editor and have the change applied",
      "Aggregated logs read from the engine, one stream per container, prefixed by service",
    ],
    frame: "compose",
    frameTitle: "hublot: compose",
  },
];

export type Feature = { title: string; body: string };

export const features: Feature[] = [
  {
    title: "Figures that match docker stats",
    body: "CPU derived from the cumulative counters the way the CLI does it, memory with the page cache subtracted, and a dash rather than a fake zero on the first sample of a stream.",
  },
  {
    title: "A shell that opens on a clean screen",
    body: "bash inside the container when the image has one, sh otherwise, on the terminal's own alternate buffer: it does not open on top of your scrollback and leaves nothing behind.",
  },
  {
    title: "Reading happens beside the list",
    body: "Logs, inspect output and resolved configuration open in a panel next to what they are about, with their own search and follow. Drag the divider, or let it move below the list on a narrow terminal.",
  },
  {
    title: "Failures that explain themselves",
    body: "A port already bound says which container holds it. A compose file that will not parse offers to open it in your editor, then to run again what failed. The daemon's own message stays underneath, never rewritten.",
  },
  {
    title: "Create, not only watch",
    body: "A container from a form that asks what docker run takes, a volume or a network from a prompt, an image pulled by name, and a compose stack written from scratch in your own editor.",
  },
  {
    title: "Filter, sort, select",
    body: "One table across every view: incremental filtering, sorting on any column, multi-selection for batch actions, and columns that drop themselves on a narrow terminal.",
  },
  {
    title: "Read-only mode",
    body: "A flag that disables every mutating action and says so in the status bar, so it is safe to open on a server just to look.",
  },
  {
    title: "Events, not polling",
    body: "The daemon event stream is the source of truth, with backoff reconnection and a full resync afterwards, because events are missed while it is down.",
  },
  {
    title: "Local daemon only",
    body: "No contexts, no TCP, no SSH. It talks to the socket on the machine it runs on, which keeps the whole thing small and predictable.",
  },
];

/** Asks GitHub which release is current, so no version is written down here
 *  to go stale. It is two statements on one line because it is one idea. */
const RESOLVE_VERSION =
  "version=$(curl -fsSLI -o /dev/null -w '%{url_effective}' " +
  "https://github.com/RobinHil/hublot/releases/latest | sed 's|.*/tag/v||'); " +
  "base=https://github.com/RobinHil/hublot/releases/download/v$version";

export type Install = {
  id: string;
  label: string;
  note: string;
  commands: string[];
};

export const installs: Install[] = [
  {
    id: "arch",
    label: "Arch Linux",
    note: "Builds from source through the PKGBUILD, which every release is tested against.",
    commands: [
      "git clone https://github.com/RobinHil/hublot.git",
      "cd hublot/packaging/aur && makepkg -si",
    ],
  },
  {
    id: "debian",
    label: "Debian, Ubuntu",
    note: "Release files carry their version, so the first line asks which one is current.",
    commands: [
      RESOLVE_VERSION,
      "curl -LO $base/hublot_${version}_amd64.deb",
      "sudo apt install ./hublot_${version}_amd64.deb",
    ],
  },
  {
    id: "fedora",
    label: "Fedora, RHEL",
    note: "Release files carry their version, so the first line asks which one is current.",
    commands: [
      RESOLVE_VERSION,
      "curl -LO $base/hublot-${version}-1.x86_64.rpm",
      "sudo dnf install ./hublot-${version}-1.x86_64.rpm",
    ],
  },
  {
    id: "appimage",
    label: "Any distribution",
    note: "The AppImage needs nothing installed: make it executable and run it.",
    // Two lines rather than one chained with `&&`, because uBlock Origin
    // refuses a clipboard write where a line starting with `curl` is followed
    // by `chmod +x` and `&&`, which is what a ClickFix attack looks like. It
    // does not fail quietly either: it covers the page with a warning about an
    // attack, which is the last thing an install page should say.
    commands: [
      RESOLVE_VERSION,
      "curl -LO $base/hublot-${version}-x86_64.AppImage",
      "chmod +x hublot-*.AppImage",
      "./hublot-*.AppImage",
    ],
  },
  {
    id: "source",
    label: "From source",
    note: "Needs the Go toolchain go.mod asks for. macOS is supported the same way.",
    commands: [
      "git clone https://github.com/RobinHil/hublot.git",
      "cd hublot && make build && ./dist/hublot",
    ],
  },
];

export const keys: { key: string; description: string }[] = [
  { key: "1..6", description: "switch view" },
  { key: "arrows", description: "move, and change view sideways" },
  { key: "/", description: "filter as you type" },
  { key: "space", description: "select for a batch action" },
  { key: "l", description: "logs, beside the list" },
  { key: "e", description: "shell inside the container" },
  { key: "ctrl+w", description: "move between the list and the panel" },
  { key: "x", description: "the action palette, for everything rarer" },
  { key: "?", description: "every binding, generated from the source" },
];
