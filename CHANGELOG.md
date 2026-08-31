# Changelog

Notable changes to Raven. Newest first. Dates are when the change shipped as a
downloadable build.

## Unreleased

Nothing pending.

## 2026-06-14

Fixed a bug where sending six or more files, or a folder with that many inside, would
transfer the first few and then fail. A network timeout added for safety was written
so that its deadline, set once when the connection opened, also applied to the
one-byte acknowledgement the receiver sends after each file. Once that deadline
elapsed, the acknowledgement write failed and the rest of the batch broke with it. The
failure was time-based, not count-based, which is why it showed up on larger batches
that took longer to run. The fix removes any deadline from the file transfer itself,
so a large transfer over slow Wi-Fi is never cut off, and gives each small control
message its own fresh deadline. A regression test reproduces the exact condition and
fails without the fix.

## 2026-06-03

Added the ability to cancel a transfer in progress, from either the sending or the
receiving side. Cancelling closes the connection cleanly, and a partly-written file on
the receiver is deleted rather than left behind corrupted.

The sender now shows a waiting state while it waits for the receiver to accept, instead
of a blank screen with no indication anything is happening. Pairing shows the same
waiting state after you confirm the code, until the other device confirms too.

A send to an unpaired device no longer fails and makes you start over. It remembers the
files, walks you through pairing, and then continues the send automatically.

Transfer history gained search, a toggle to hide file names for privacy, collapsible
day sections, and expandable folder entries that list their contents in place. A
received file whose saved copy has since been moved or deleted is shown struck through.
The live transfer view now shows the paired device's name rather than its raw address,
and a folder transfer tracks progress across all its files as one continuous bar
instead of restarting for each file.

## 2026-06-02

First public builds, for macOS as a universal app that runs on both Intel and Apple
Silicon, and for Windows. Both are unsigned.

This release replaced the earlier plaintext protocol entirely with an encrypted,
authenticated one. Every transfer now runs inside a mutual TLS 1.3 session. Each device
has its own key and identity fingerprint, devices must pair before they can transfer,
and pairing is confirmed by comparing a six-digit code on both screens. A device whose
key has changed is refused rather than trusted. The accept prompt still authorizes each
individual transfer, on by default, with an option to auto-accept from paired devices.

Raven began as a single-file command-line tool for direct file transfer over a local
network. This release wrapped that core in a desktop app built with Wails, added
drag-to-send, folder transfers that keep their structure, a device list, and a transfer
history, while keeping the command-line tool working against the same shared,
standard-library-only core.
