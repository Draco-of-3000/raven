# Raven

**Raven moves a file straight from one of your machines to another over the local
network, encrypted, with nothing in between.**

## The problem

You buy a new Mac. It has USB-C ports and nothing else. Your external drive, the
one with everything you actually need on it, is USB-A. So the two things you own
cannot talk to each other without a dongle you do not have.

The workarounds are all worse than they should be. Buy an adapter for a job you will
do twice. Reformat the drive and lose what is on it. Upload forty gigabytes to a
cloud service so you can immediately download it again onto a machine sitting three
feet away, assuming your upload speed and their storage limit both cooperate. Email
yourself a file and hit the attachment cap. Every one of these routes a transfer
between two devices in the same room through somewhere else entirely.

The devices are already on the same Wi-Fi. The file should just go.

## What Raven does

Raven is a small desktop app. Open it on both machines, drag a file onto the window,
pick the other device, and the file lands on it. Folders keep their structure.
Nothing touches the internet, so the speed you get is your Wi-Fi and your disks, not
your upload plan, and the transfer works even with the internet unplugged.

The first time two devices meet they pair, once. Both screens show the same six-digit
code and you confirm it matches. After that they recognize each other and you just
send. Incoming files always ask before they land: you see who is sending and exactly
what, then accept or decline.

There is also a command-line version that speaks the same protocol and pairs with the
desktop app, for when a machine has no screen or you want to script it.

## Security

Every transfer runs inside a mutual TLS 1.3 session. TLS is the same protocol that
encrypts websites; mutual means both ends prove who they are, not just the receiver.
So the bytes are encrypted the whole way across the network and each side is
authenticated to the other.

Each device generates its own key the first time it runs. Its identity is the
fingerprint of that key, a SHA-256 hash, which is unique to the device and cannot be
forged without its private key. Pairing is how two devices exchange and remember each
other's fingerprint. The six-digit code you compare during pairing is derived from
both fingerprints plus a secret unique to that one connection, so if an attacker sat
in the middle presenting fake keys, the two codes would not match and you would see
it. This is the one moment security depends on a human looking, which is why the code
is front and center.

After pairing, every connection checks the other device's fingerprint against the one
you stored. An unknown device cannot transfer to you, and if a paired device's key
ever changes, which is what an impersonation attempt looks like, the connection is
refused outright rather than trusted silently. Each file also carries a SHA-256
checksum that the receiver verifies before keeping it, so a file that arrived
corrupted is discarded rather than saved.

Two things this does not protect against, stated plainly. If you confirm the pairing
code without actually comparing it to the other screen, you have skipped the one check
that stops a man in the middle, so look at it. And pairing authenticates a device, not
the person at it: a device you trust can still send you a file you did not want, which
is why the accept prompt asks every time and can be left on.

The whole transfer core is written against the Go standard library, including all the
cryptography. There are no third-party crypto dependencies to audit or trust.

## Install

Both builds are unsigned, because a code-signing certificate costs money I have not
spent on a side project. That means the operating system will warn you once on first
launch. This is expected and the steps below get past it.

On macOS, open the `.dmg`, drag Raven into Applications, then right-click it and choose
Open. A plain double-click is blocked the first time because the app is unsigned;
right-click Open is the one-time way through. macOS will also ask permission to find
devices on your local network. Allow it, or other machines cannot reach you.

On Windows, run the `.exe`. SmartScreen will show a warning; click More info, then Run
anyway. Windows Firewall will ask whether to allow Raven on your network. Allow it on
private networks so discovery works both ways.

## Build from source

You need Go 1.23 or newer, Node 18 or newer, and a C toolchain, which on macOS means
the Xcode Command Line Tools. Raven is built with Wails, a framework for building
desktop apps with a Go backend and a web frontend, so you also need the Wails command.

Install Go and the Wails command:

```bash
brew install go
```

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Check your environment is complete. This reports anything missing:

```bash
wails doctor
```

Run the app with live reload while developing:

```bash
wails dev
```

Produce a distributable app. This writes to `build/bin`:

```bash
wails build
```

Run the test suite. The core protocol, pairing, and the safety checks are covered:

```bash
go test ./...
```

## The command-line version

Build it as a standalone binary that depends only on the Go standard library:

```bash
go build -o raven-cli ./cmd/raven-cli
```

Start a receiver, which prints its address and waits:

```bash
raven-cli receive
```

Pair with another device once, comparing the six-digit code on both ends:

```bash
raven-cli pair -to 192.168.1.42
```

Send files and folders to a paired device:

```bash
raven-cli send -to 192.168.1.42 report.pdf ./photos
```

List the devices you have paired with:

```bash
raven-cli devices
```

## How it works

The desktop app is a Go program with a web interface, bridged by Wails. The Go side
handles all networking, pairing, and file writing. The interface is React, and it
talks to Go by calling exposed methods and listening for events the Go side emits as
a transfer progresses. Because the two share the transfer core, the command-line
version and the app are the same protocol wearing different faces.

Finding devices on the network is a separate, deliberately untrusted step. A device
that wants to send broadcasts a question over UDP, and receivers answer with their
name and port. This only suggests candidates. It grants no trust, because trust comes
only from the pinned fingerprint checked when a connection is actually made. A spoofed
discovery reply gets you nothing: the transfer still fails the fingerprint check.

The wire protocol, named GOXFER03, lives inside the TLS session. After the handshake
the sender writes a short header, a byte saying whether this is a pairing or a
transfer, and then for a transfer: the number of files, a small JSON description of
each one, a one-byte accept or decline from the receiver, and finally the file bodies,
each followed by its checksum. Only the small control messages between files have time
limits on them. The file body itself has none, so a hundred-gigabyte folder over slow
Wi-Fi takes as long as it takes and is never cut off partway.

## Layout

The transfer core is `internal/transfer`, standard library only, shared by both the
app and the command-line tool. `cmd/raven-cli` is the command-line tool. `main.go` and
`app.go` are the Wails app: the window, the methods the interface can call, the events
it emits, and where settings are stored. `frontend` is the React interface. The
product and interaction design is written up in `docs/DESIGN.md`, and the licenses for
the bundled fonts and icon are in `ATTRIBUTION.md`.

Your device identity, your paired devices, and your settings are stored per user,
outside the app, so they survive an update. On macOS that is
`~/Library/Application Support/Raven`. Transfer history stays on each device and is
never sent anywhere.

## Known limitations

The apps are unsigned, so first launch requires the one-time steps under Install.
Signing needs paid certificates from Apple and a Windows certificate authority, which
is a cost decision, not a code one.

Discovery can be one-directional. If one machine sees the other but not the reverse,
it is almost always a firewall or a network set to Public rather than Private on the
machine that cannot be seen, not a bug in Raven. Some networks, particularly guest
Wi-Fi, block device-to-device traffic entirely. In all of these cases you can still
send by typing the other device's address directly, which does not rely on discovery.

The animation that plays during a transfer is a placeholder whose license is not yet
settled, noted in `ATTRIBUTION.md`. The interface fonts currently load from Google
Fonts rather than being bundled, so the app is not fully offline for its typography
yet. Neither affects transfers.
