# Raven, Product Design Spec

Status: draft for review. Date: 2026-05-31.

## 1. What Raven is

Raven is a native desktop app for direct device to device file transfer over the local
network. It wraps an existing Go command line tool (UDP discovery, a small framed TCP
protocol, SHA-256 integrity check, standard library only) in a single calm window built
with Wails. No cloud, no accounts; files move straight between machines on the same Wi-Fi.

Design intent: minimal and lightweight, but premium and world class. Restraint over
decoration. One delightful flourish, a raven that flies the file to its destination.

## 2. Brand and visual language

- Name: Raven.
- Mood: support both light and dark. Default is Parchment (warm light): paper `#f7f6f3`,
  ink `#15161b`, hairline warm-gray borders. Graphite (dark) is the alternate: near-black
  with soft off-white text and a cool accent.
- No gradients anywhere. Flat fills, hairline dividers, soft shadow only for window elevation.
- Typography: Space Grotesk for headings and the wordmark, Inter for body and UI text,
  system monospace for addresses and sizes.
- Accent: a single restrained indigo, used sparingly (active progress, links, selected
  state). A calm green dot for Listening status.
- Icon (mark): Phosphor "Bird" (MIT), recolored to theme ink. Minimal and clean.
- In-flight animation: an animated raven Lottie (minimal monochrome silhouette) that flaps
  as it crosses the stage. Placeholder in the mockups for now; final asset and license TBD
  (see `ATTRIBUTION.md`).
- Copy rules: no em-dashes; security wording must be accurate to the trust model in the
  Security section below (encrypted in transit via TLS 1.3, authenticated to paired devices);
  platform-aware wording (macOS vs Windows).

## 3. Layout: one window, three regions

A single resizable window. Hybrid model: drop-first, detail on demand. A title bar carries
the Raven wordmark and a settings control. Below it, three regions:

1. **Stage** (primary, left, roughly 65 percent): the calm drop target. Idle shows the mark,
   "Drop files to send", and "or click to choose files". The stage also plays the live
   transfer (the raven in flight) and hosts an incoming request when one arrives.
2. **Rail** (right, roughly 35 percent, collapsible): the live picture.
   - This device: friendly name, address (`ip:port`), a softly pulsing "Listening" status.
   - Devices nearby: discovered devices as flat rows, plus "Add by address" for manual targets.
3. **History** (full width, bottom): completed transfers grouped by day; each row shows
   direction (sent or received), file name, peer, size, and time. Date-range filters narrow
   the list: This week, Last week, This month, All time.

Everything is flat: hairline separators, generous whitespace, no card or box components.

## 4. Core flows

### Send
1. Drop file(s) or folder(s) on the stage, or click to choose. Folders are sent recursively
   and rebuilt with their structure intact on the receiving side (path traversal is blocked,
   and a colliding folder name is renamed once, e.g. "photos" to "photos (1)").
2. Pick the destination from the rail's nearby devices, or use Add by address.
3. The stage plays the flight: the raven crosses from You to the peer with its horizontal
   position bound to actual transfer progress, so it arrives at the destination only when the
   transfer completes, never early. Wings flap (the Lottie). A flat progress bar tracks the
   same bytes, with file name, size, speed, and time remaining. The flyer is modestly sized.
4. On completion the transfer drops into History.

### Receive (prompt to accept)
1. An incoming request takes over the stage: sender name, the exact file list with sizes,
   total size, and the from-address.
2. Actions: Accept & save (one ink button) or Decline (ghost button). Every incoming
   transfer prompts, every time. There is no "always accept" or trusted-device option.
3. On Accept, files save to `~/Downloads/Raven` and the inbound flight plays (the raven flies
   toward you), then lands in History. Decline dismisses.

### Onboarding (first launch)
Three quiet steps, premium and minimal:
1. Welcome: the mark, "Welcome to Raven", and "Send files straight between your devices on the
   same network. No cloud, no accounts."
2. Your device: set a friendly device name (prefilled), choose the save folder (default
   `~/Downloads/Raven`).
3. Stay reachable: a platform-aware network note (macOS Local Network permission, or Windows
   Firewall on private networks) and "You'll listen as [name] at [ip:port]".

After the last step, the window becomes the main app.

## 5. Behavior decisions (locked)

- Default save location: `~/Downloads/Raven` (created on first run), changeable, persisted.
- Incoming: always prompt by default; a setting can auto-accept transfers from paired devices.
- Pairing is required before any transfer (see Security).
- Discovery: UDP broadcast, plus manual Add by address. Discovery is untrusted (it only suggests
  candidates; trust comes from pairing).
- Light and dark both supported; Parchment is the default.

## 5a. Security (the trust model, stated honestly)

Raven transfers are **encrypted in transit (TLS 1.3)** and **authenticated to paired devices**.

- **Identity:** each device generates a self-signed ECDSA P-256 certificate on first run. The
  identity is the SHA-256 of its public key (the "fingerprint"). Files are encrypted by TLS;
  authentication is by pinning the fingerprint, not by any certificate authority.
- **Pairing (one time per device pair):** the two devices show the same 6-digit code (a Short
  Authentication String derived from both fingerprints plus the TLS session secret). The user
  compares the codes; matching codes prove there is no man-in-the-middle. The code is computed
  locally and never sent. After pairing, each device remembers the other's fingerprint.
- **Every transfer** verifies the peer's pinned fingerprint; an unpaired or changed identity is
  rejected (a changed key shows a loud "identity changed" failure). Files are still SHA-256
  checked on arrival as a disk-integrity guard.
- **Protects against:** passive sniffing on the LAN; active man-in-the-middle after pairing;
  tampering.
- **Does NOT protect against:** a man-in-the-middle at first pairing if the user confirms without
  actually comparing the code (the human comparison is the security); a paired device that itself
  sends malicious files (which is why the accept prompt stays); theft of the local private key
  (stored owner-only, `0o600`, no passphrase in this version). Discovery spoofing is harmless
  because connecting still requires passing the pinned-fingerprint check.
- **Accept prompt vs pairing:** pairing authenticates the *device*; the accept prompt authorizes
  the *transfer*. Default is prompt-every-time; "auto-accept from paired devices" is an opt-in
  setting.
- **Hardening:** wire-driven allocations and file counts are bounded; idle connections time out;
  concurrent receive sessions are capped; filenames are stripped of control characters and Unicode
  bidi overrides so a name cannot disguise its extension. Verified by `internal/transfer/*_test.go`.

## 6. Settings

Reached from the title bar control. A flat list grouped with hairlines, with a Done action.

- Identity: Device name (how you appear to others nearby).
- Files: Save received files to (path, with Change), default `~/Downloads/Raven`.
- Appearance: Theme, segmented Parchment / Graphite / System.
- About: version and an Acknowledgements link (shows the `ATTRIBUTION.md` credits).
- Advanced (collapsed by default): Listen port (default 51888). The port is the network
  "door" Raven listens on for incoming transfers. It is rarely changed (only on a port
  conflict), and discovery shares it with senders automatically, so it stays tucked away to
  keep the main surface clean.

## 7. States

All flat, calm copy, no over-claims.

- Looking for devices: a quiet spinner with "Looking for devices" and "No devices nearby yet.
  Open Raven on the other device, or add it by address," plus an Add by address action.
- Offline: an offline glyph with "You're offline" and "Connect to Wi-Fi to send and receive
  files"; the status reads Offline.
- Transfer error: "Couldn't reach [device]" (the device may be asleep or off the network)
  with Dismiss and Retry; and a checksum-mismatch failure, "File failed its integrity check
  and was discarded. Retry?"
- Empty history: "No transfers yet."
- Large-file progress: the flight and progress bar carry long transfers; speed and time
  remaining update live.
- Window close mid-idle: the receiver stops cleanly.

## 8. Assets and licensing

See `ATTRIBUTION.md`. Mark: Phosphor "Bird" (MIT, no visible credit required). Fonts: Space
Grotesk and Inter (SIL Open Font License 1.1). Raven Lottie: license is per-asset and must be
confirmed before release.

## 9. Technical foundation (engineering plan follows)

Raven wraps the existing single-file Go CLI. The protocol logic (discovery, framed TCP send
and receive, SHA-256) will be extracted into a reusable, IO-agnostic core shared by both a
thin command line binary and the Wails GUI. The GUI reports progress and lifecycle as events
and uses native file drop. A detailed implementation plan (module structure, Wails wiring,
and toolchain setup of Go and the Wails CLI via Homebrew) is the next deliverable.

## 10. Open items

- Final raven Lottie selection and its license (placeholder in mockups now).
- Mark versus flight cohesion: the mark is a generic Phosphor bird while the flight is a
  raven. Revisit if we want to unify them (for example reuse the Lottie raven as the mark).
- Settings and edge-case states need their own detailed screens.

## Reference: mockups

Interactive mockups produced during design live under `.superpowers/brainstorm/` (layout
directions, mood and type, main window with rail and filterable history, live transfer flight,
accept prompt, onboarding). These are scratch artifacts, not shipped code.
