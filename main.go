package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func showUsage(out *os.File) {
	prog := filepath.Base(os.Args[0])
	fmt.Fprintln(out, "Usage:", prog, "action [args ...]")
	fmt.Fprintln(out, "If you want to see action-specific help message:")
	fmt.Fprintln(out, "  $", prog, "action -help")
	fmt.Fprintln(out, "")
	showActions(out)
}

func showActions(out *os.File) {
	fmt.Fprintln(out, "Supported actions:")
	for name, act := range actions {
		fmt.Fprintf(out, "  %-10s: %s\n", name, act.Help)
	}
}

type CommandAction struct {
	Func func ([]string)
	Help string
}

var (
	actions = map[string]CommandAction {
		"server": {
			Func: serverMain,
			Help: "Starts clipboard server.",
		},
		"client": {
			Func: clientMain,
			Help: "Executes clipboard program.",
		},
	}
)

func main() {

	flags := flag.NewFlagSet("main", flag.ContinueOnError)
	help := flags.Bool("help", false, "show this help.")
	err := flags.Parse(os.Args[1:])

	if err != nil {
		showUsage(os.Stderr)
		os.Exit(1)
	} else if *help {
		showUsage(os.Stdout)
		os.Exit(0)
	} else if flags.NArg() == 0 {
		fmt.Fprint(os.Stderr, "Action must be specified.\n\n")
		showUsage(os.Stderr)
		os.Exit(1)
	}

	actName := flags.Arg(0)

	act, ok := actions[actName]
	if !ok {
		fmt.Fprintln(os.Stderr, "Unknown action:", actName)
		showActions(os.Stderr)
		os.Exit(1)
	}
	act.Func(flags.Args()[1:])
}

func serverMain(args []string) {
	flags := flag.NewFlagSet("server", flag.ExitOnError)
	addr := flags.String("addr", ":8000", "specify listen address and port.")
	flags.Parse(args)

	// check OS
	if _, ok := clipWriters[runtime.GOOS]; !ok {
		log.Fatal("Unsupported OS: ", runtime.GOOS)
	}

	listener, err := net.Listen("tcp4", *addr)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	log.Println("Listening on", *addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Print(err)
			continue
		}

		go serve(conn)
	}
}

type WriteFuncType func ([]byte) (int, error)

func makeStdinWriter(command string, args ...string) WriteFuncType {
	return func (data []byte) (int, error) {
		cmd := exec.Command(command, args...)
		writer, err := cmd.StdinPipe()
		if err != nil {
			return 0, err
		}

		n, err := writer.Write(data)
		if err != nil {
			return n, err
		}

		err = writer.Close()
		if err != nil {
			return n, err
		}

		err = cmd.Run()
		if err != nil {
			return n, err
		}

		return n, nil
	}
}

var (
	clipWriters = map[string]WriteFuncType {
		"windows": makeStdinWriter(`C:\Windows\System32\clip.exe`),
		"darwin": makeStdinWriter("/usr/bin/pbcopy"),
	}
)

func serve(conn net.Conn) {
	buf := new(bytes.Buffer)
	tmp := make([]byte, 1024)
	for {
		n, err := conn.Read(tmp)
		if err != nil {
			if n != 0 && err != io.EOF {
				log.Print(err)
			}
			break
		}
		buf.Write(tmp[:n])
	}

	writer := clipWriters[runtime.GOOS]
	n, err := writer(buf.Bytes())
	if err != nil {
		log.Print(err)
		return
	}

	log.Print(n, " bytes trasfered to clipboard.")
}

func writeAllBytes(dest io.Writer, src []byte) error {
	written := 0
	for written < len(src) {
		n, err := dest.Write(src[written:])
		if err != nil {
			return err
		}
		written += n
	}
	return nil
}

func clientMain(args []string) {
	flags := flag.NewFlagSet("client", flag.ExitOnError)
	addr := flags.String("addr", "localhost:8000",
		"specify address and port to connect.")
	flags.Parse(args)

	conn, err := net.DialTimeout("tcp4", *addr, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	log.Println("connected to", conn.RemoteAddr())

	//written, err := io.Copy(conn, os.Stdin)
	var written int64 = 0
	buf := make([]byte, 1024)
Loop:
	for {
		n, err := os.Stdin.Read(buf)
		switch {
		case n < 0:
			log.Fatal(err)
		case n == 0:
			break Loop
		case n > 0:
			writeAllBytes(conn, buf[:n])
			written += int64(n)
		}
	}

	log.Print(written, " bytes were transfered.")
}
