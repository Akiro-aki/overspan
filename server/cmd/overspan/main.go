// Command overspan turns phones, tablets and other computers into extra
// monitors for a Linux desktop, over the local network, viewed in a browser.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"overspan/internal/input"
	"overspan/internal/server"
	"overspan/internal/vdisplay"
	"overspan/internal/web"
)

const udevRule = `# Overspan: let the logged-in user create virtual input devices
KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"
`

func usage() {
	fmt.Fprint(os.Stderr, `Overspan: use other devices as extra monitors for this computer.

Usage:
  overspan [serve] [--port 7940] [--backend auto|hyprland|sway|x11|wlroots] [--open]
  overspan doctor        check what works in this session
  overspan outputs       list monitors in the current layout
  overspan install-udev  allow touch, mouse and keyboard input (asks for sudo)
  overspan version

Then open http://localhost:7940/admin on this computer, and the address it
shows on the other device.
`)
}

func main() {
	log.SetFlags(log.Ltime)
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		serve(args)
	case "doctor":
		doctor()
	case "outputs":
		b, note := vdisplay.Detect()
		outs, err := b.Outputs()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("backend: %s (%s)\n", b.Name(), note)
		for _, o := range outs {
			fmt.Printf("  %-16s %5dx%-5d at %d,%d  scale %.2f\n", o.Name, o.PxW, o.PxH, o.X, o.Y, o.Scale)
		}
	case "install-udev":
		installUdev()
	case "version":
		fmt.Println("overspan", server.Version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 7940, "TCP port for viewers (UDP port+1 answers discovery)")
	backend := fs.String("backend", "auto", "virtual monitor backend")
	open := fs.Bool("open", false, "open the dashboard in a browser")
	fs.Parse(args)

	dash := fmt.Sprintf("http://localhost:%d/admin", *port)
	// Bind before creating input devices, so a second launch (say from the
	// app menu) just opens the dashboard of the instance already running.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		if *open {
			fmt.Println("Overspan is already running; opening its dashboard.")
			exec.Command("xdg-open", dash).Run()
			return
		}
		log.Fatalf("cannot listen on port %d: %v (is Overspan already running?)", *port, err)
	}
	srv, err := server.New(*port, web.FS(), *backend)
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.ServeDiscovery(); err != nil {
		log.Printf("discovery off: %v", err)
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go hs.Serve(ln)

	fmt.Printf("\nOverspan %s is running.\n  Dashboard (this computer): %s\n", server.Version, dash)
	for _, a := range srv.Addresses() {
		fmt.Printf("  Viewers (other devices):   %s\n", a)
	}
	fmt.Println()
	if *open {
		exec.Command("xdg-open", dash).Start()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("\nStopping: removing virtual monitors...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Close()
	hs.Shutdown(ctx)
}

func doctor() {
	b, note := vdisplay.Detect()
	var injErr error
	if inj, err := input.Open(); err != nil {
		injErr = err
	} else {
		inj.Close()
	}
	marks := map[string]string{"ok": "\033[32m✓\033[0m", "warn": "\033[33m!\033[0m", "fail": "\033[31m✗\033[0m"}
	bad := false
	for _, c := range server.RunDoctor(b, note, injErr) {
		fmt.Printf(" %s %-26s %s\n", marks[c.Status], c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Printf("   %-26s → %s\n", "", c.Fix)
		}
		bad = bad || c.Status == "fail"
	}
	if bad {
		os.Exit(1)
	}
}

func installUdev() {
	const path = "/etc/udev/rules.d/70-overspan-uinput.rules"
	fmt.Println("Writing", path, "(needs sudo):")
	fmt.Print(udevRule)
	cmd := exec.Command("sudo", "sh", "-c", fmt.Sprintf("cat > %s && modprobe uinput && udevadm control --reload && udevadm trigger --name-match=uinput", path))
	cmd.Stdin = strings.NewReader(udevRule)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "failed:", err)
		os.Exit(1)
	}
	if err := os.WriteFile("/dev/null", nil, 0); err == nil {
		fmt.Println("Done. If input still fails, log out and back in once.")
	}
}
