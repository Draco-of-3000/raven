// raven-cli is the standard-library-only command line for Raven's transfer core.
// All transfers are encrypted (mutual TLS) and require devices to be paired first.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"raven/internal/transfer"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "receive", "recv", "r":
		cmdReceive(os.Args[2:])
	case "send", "s":
		cmdSend(os.Args[2:])
	case "pair", "p":
		cmdPair(os.Args[2:])
	case "devices":
		cmdDevices(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`raven: direct, encrypted file transfer over WiFi/LAN

USAGE
  raven receive [-port N] [-dir PATH] [-name LABEL]
  raven pair   [-to IP[:PORT]] [-timeout SEC]
  raven send   [-to IP[:PORT]] [-timeout SEC] <path> [path...]
  raven devices

Devices must be PAIRED before they can transfer. Pairing shows a 6-digit code on
both devices; confirm they match. All transfers are encrypted (TLS 1.3).

RECEIVE
  Starts a receiver. Prints your LAN address and waits to pair / receive.
  -port N     TCP port to listen on (default 51888)
  -dir PATH   directory to save files into (default: current directory)
  -name LABEL friendly name shown during discovery (default: hostname)

PAIR
  Pairs with another device so you can transfer to/from it.
  -to IP       device address (host or host:port); empty = auto-discover

SEND
  Sends files and/or folders to a PAIRED receiver. Folders keep their structure.
  -to IP       send directly to this receiver (host or host:port)
  -timeout SEC discovery wait time in seconds (default 3)

EXAMPLES
  raven receive
  raven pair -to 192.168.1.42
  raven send -to 192.168.1.42 report.pdf ./photos
`)
}

// identityDir returns the shared config dir (same as the GUI) so the CLI and GUI
// use the same identity + paired store.
func identityDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "Raven")
}

func loadCLIIdentity(name string) (*transfer.Identity, *transfer.PairedStore) {
	dir := identityDir()
	id, err := transfer.LoadOrCreateIdentity(dir, name)
	if err != nil {
		fatal("identity: %v", err)
	}
	store := transfer.LoadPairedStore(filepath.Join(dir, "paired.json"))
	return id, store
}

// cliConfirmSAS prints the pairing code and reads y/n from stdin.
func cliConfirmSAS(peerName, code string) bool {
	fmt.Printf("\n  Pairing with %q\n", transfer.CleanText(peerName))
	fmt.Printf("  Verification code: %s\n", code)
	fmt.Printf("  Does this match the code on the other device? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

func cmdReceive(args []string) {
	fs := flag.NewFlagSet("receive", flag.ExitOnError)
	port := fs.Int("port", transfer.DefaultTCP, "TCP port to listen on")
	dir := fs.String("dir", ".", "directory to save received files into")
	host, _ := os.Hostname()
	name := fs.String("name", host, "friendly name shown during discovery")
	_ = fs.Parse(args)

	id, store := loadCLIIdentity(*name)
	rcv := &transfer.Receiver{
		Dir: *dir, Name: *name, Port: *port, Obs: &stdoutObserver{},
		Identity: id, Paired: store, OnPair: cliConfirmSAS,
	}
	if err := rcv.Listen(); err != nil {
		fatal("%v", err)
	}
	ip, p := rcv.LocalAddr()
	fmt.Printf("\n  raven receiver ready\n")
	fmt.Printf("  ----------------------------------------\n")
	fmt.Printf("  Name:        %s\n", *name)
	fmt.Printf("  Address:     %s:%d\n", ip, p)
	fmt.Printf("  Saving to:   %s\n", absOr(*dir))
	fmt.Printf("  Fingerprint: %s\n", transfer.ShortFingerprint(id.FP))
	fmt.Printf("  ----------------------------------------\n")
	fmt.Printf("  Waiting to pair / receive... (Ctrl+C to stop)\n\n")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	_ = rcv.Serve(ctx)
}

func cmdPair(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	to := fs.String("to", "", "device address (host or host:port); empty = auto-discover")
	timeout := fs.Int("timeout", 3, "discovery wait time in seconds")
	host, _ := os.Hostname()
	name := fs.String("name", host, "this device's name")
	_ = fs.Parse(args)

	id, store := loadCLIIdentity(*name)
	target := resolveTarget(*to, *timeout)
	fmt.Printf("connecting to %s to pair...\n", target)
	dev, err := transfer.Pair(context.Background(), target, id, *name, cliConfirmSAS, 30*time.Second)
	if err != nil {
		fatal("pairing failed: %v", err)
	}
	dev.PairedAt = time.Now().Unix()
	dev.LastSeen = dev.PairedAt
	_ = store.Add(dev)
	fmt.Printf("  paired with %q [%s]\n", transfer.CleanText(dev.Name), transfer.ShortFingerprint(dev.Fingerprint))
}

func cmdDevices(args []string) {
	host, _ := os.Hostname()
	_, store := loadCLIIdentity(host)
	list := store.List()
	if len(list) == 0 {
		fmt.Println("no paired devices.")
		return
	}
	fmt.Printf("paired devices:\n")
	for _, d := range list {
		fmt.Printf("  %-24s %s\n", transfer.CleanText(d.Name), transfer.ShortFingerprint(d.Fingerprint))
	}
}

func resolveTarget(to string, timeout int) string {
	if to != "" {
		return to
	}
	fmt.Printf("searching for a device on the network...\n")
	found, err := transfer.Discover(context.Background(), time.Duration(timeout)*time.Second)
	if err != nil || len(found) == 0 {
		fatal("no device found. Make sure 'raven receive' is running on the\n" +
			"other machine, or use -to <IP>")
	}
	r := found[0]
	fmt.Printf("found %q at %s\n", transfer.CleanText(r.Name), r.Addr)
	if len(found) > 1 {
		fmt.Printf("(note: %d devices found; using the first)\n", len(found))
	}
	return r.Addr
}

func cmdSend(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	to := fs.String("to", "", "receiver address (host or host:port); empty = auto-discover")
	timeout := fs.Int("timeout", 3, "discovery wait time in seconds")
	host, _ := os.Hostname()
	name := fs.String("name", host, "this device's name")
	_ = fs.Parse(args)

	files := fs.Args()
	if len(files) == 0 {
		fatal("no files given. usage: raven send [-to IP] <path> [path...]")
	}

	id, store := loadCLIIdentity(*name)
	target := resolveTarget(*to, *timeout)
	fmt.Printf("connecting to %s\n\n", target)
	err := transfer.SendV3(context.Background(), target, files, id, store, &stdoutObserver{}, 30*time.Second)
	if err == transfer.ErrNotPaired {
		fatal("not paired with this device. Run: raven pair -to %s", target)
	}
	if err != nil {
		fatal("%v", err)
	}
}

// stdoutObserver renders transfer progress to the terminal.
type stdoutObserver struct {
	total int
}

func (o *stdoutObserver) WaitingForAccept(peer string) {
	fmt.Printf("waiting for %s to accept...\n", transfer.CleanText(peer))
}

func (o *stdoutObserver) SessionStart(dir transfer.Direction, peer string, count int, totalBytes int64) {
	o.total = count
	if dir == transfer.Receiving {
		fmt.Printf("> connection from %s\n", peer)
	}
}

func (o *stdoutObserver) FileStart(dir transfer.Direction, idx, total int, name string, size int64) {
	fmt.Printf("  [%d/%d] %s (%s)\n", idx, total, transfer.CleanText(name), transfer.HumanBytes(size))
}

func (o *stdoutObserver) Progress(p transfer.FileProgress) {
	label := "sending  "
	if p.Dir == transfer.Receiving {
		label = "receiving"
	}
	pct := 0.0
	if p.Size > 0 {
		pct = float64(p.Bytes) / float64(p.Size) * 100
	}
	fmt.Printf("\r    %s [%s] %5.1f%%  %s/s   ", label, makeBar(pct, 24), pct, transfer.HumanBytes(p.BytesPerSec))
}

func (o *stdoutObserver) FileDone(r transfer.FileResult) {
	fmt.Print("\n")
	if r.Err != nil {
		fmt.Printf("    failed: %v\n", r.Err)
		return
	}
	if r.Dir == transfer.Receiving {
		fmt.Printf("    saved -> %s  [verified]\n", r.SavedTo)
	} else {
		fmt.Printf("    delivered  [verified]\n")
	}
}

func (o *stdoutObserver) SessionEnd(dir transfer.Direction, peer string, err error) {
	if err != nil {
		return // file-level failures are already printed by FileDone
	}
	if dir == transfer.Receiving {
		fmt.Printf("  all %d file(s) received.\n\n", o.total)
	} else {
		fmt.Printf("\nall %d file(s) sent.\n", o.total)
	}
}

func makeBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
}

func absOr(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
	os.Exit(1)
}
