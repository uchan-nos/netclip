package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"os/signal"

	"golang.org/x/crypto/ssh"
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
	clipFifo := flags.String("clip", "$HOME/clip", "a path to fifo. default is $HOME/clip.")
	privateKey := flags.String("key", "$HOME/.ssh/id_rsa", "a path to private key file. default is $HOME/.ssh/id_rsa.")
	remoteAddr := flags.String("addr", "192.168.0.1:22", "address of remote host. default is 192.168.0.1:22.")
	netclipCmd := flags.String("netclip", "/usr/local/bin/netclip", "a path to netclip executable.")
	flags.Parse(args)

	clipWriter, ok := clipWriters[runtime.GOOS]
	// check OS
	if !ok {
		log.Fatal("Unsupported host OS: ", runtime.GOOS)
	}

	privateKeyBytes, err := ioutil.ReadFile(*privateKey)
	if err != nil {
		log.Fatal("failed to read key file: ", err)
	}
	privateKeySigner, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		log.Fatal("failed to parse key: ", err)
	}

	config := &ssh.ClientConfig{
		User: "uchan",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(privateKeySigner),
		},
	}

	client, err := ssh.Dial("tcp4", *remoteAddr, config)
	if err != nil {
		log.Fatal(err)
	}

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	for {
		session, err := client.NewSession()
		if err != nil {
			log.Fatal(err)
		}

		stdout, err := session.StdoutPipe()
		if err != nil {
			log.Fatal(err)
		}

		stdin, err := session.StdinPipe()
		if err != nil {
			log.Fatal(err)
		}

		log.Println("executing remote netclip command.")
		err = session.Start(fmt.Sprintf(`%s client "%s"`, *netclipCmd, *clipFifo));
		if  err != nil {
			log.Fatal(err)
		}

		buf := new(bytes.Buffer)
		stdoutChan := make(chan byte, 1024)
		go func() {
			err := readLoop(stdout, stdoutChan)
			log.Println(err)
		}()

		Loop:
		for {
			select {
			case c, ok := <-stdoutChan:
				if ok {
					buf.WriteByte(c)
				} else {
					session.Close()
					clipWriter(buf.Bytes())
					break Loop
				}
			case <-signalChan:
				log.Println("sending exit command to remote netclip command.")
				_, err := stdin.Write([]byte{ 0x03 })
				if err != nil {
					log.Fatal(err)
				}
				err = stdin.Close()
				if err != nil {
					log.Fatal(err)
				}
				log.Println("waiting remote netclip command to stop.")
				session.Wait()
				session.Close()
				log.Println("exiting by Interrupt.")
				os.Exit(0)
			}
		}
	}
}

func readLoop(reader io.Reader, queue chan<- byte) error {
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		for i := 0; i < n; i++ {
			queue <- buf[i]
		}
		if err != nil {
			close(queue)
			return err
		}
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
	//addr := flags.String("addr", "localhost:8000",
	//	"specify address and port to connect.")
	flags.Parse(args)

	if flags.NArg() == 0 {
		log.Fatal("a path to clip fifo is required.")
	}

	clip, err := os.Open(flags.Arg(0))
	if err != nil {
		log.Fatal("failed to open clip fifo: ", err)
	}

	stdinChan := make(chan byte, 1)
	clipChan := make(chan byte, 1024)

	go func() {
		err := readLoop(os.Stdin, stdinChan)
		log.Fatal(err)
	}()

	go func() {
		err := readLoop(clip, clipChan)
		log.Fatal(err)
	}()

	buf := new(bytes.Buffer)
	for {
		select {
		case c := <-stdinChan:
			if c == 0x03 {
				// exit command
				os.Stdout.Write(buf.Bytes())
				log.Println("exit command.")
				os.Exit(0)
			} else {
				log.Fatal("unknown command: ", c)
			}
		case c, ok := <-clipChan:
			if ok {
				buf.WriteByte(c)
			} else {
				os.Stdout.Write(buf.Bytes())
				os.Exit(0)
			}
		}
	}
}
