package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/text/transform"
	"golang.org/x/text/encoding/japanese"
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

func passwordCallback() (secret string, err error) {
	stdin := int(os.Stdin.Fd())
	if !terminal.IsTerminal(stdin) {
		log.Println("this is not a terminal")
		return "", errors.New("Can't use password authentication in non-terminal.")
	}

	fmt.Fprint(os.Stdout, "input password >")
	passBytes, err := terminal.ReadPassword(stdin)
	if err != nil {
		return "", err
	}

	return string(passBytes), nil
}

func showFlags(out *os.File, flagSet *flag.FlagSet) {
	fmt.Fprintln(out, "Options:")
	flagSet.VisitAll(func(f *flag.Flag) {
		if f.DefValue == "false" || f.DefValue == "true" {
			fmt.Fprintf(out, "  -%-12s\n          %v\n",
				f.Name, f.Usage)
		} else {
			fmt.Fprintf(out, "  -%-12s\n          %v\n",
				fmt.Sprintf("%s=%s", f.Name, f.DefValue), f.Usage)
		}
	})
}

func showServerUsage(out *os.File, flagSet *flag.FlagSet) {
	prog := filepath.Base(os.Args[0])
	fmt.Fprintln(out, "Usage:", prog, "server [options ...] remote-addr")
	fmt.Fprintln(out, "")
	showFlags(out, flagSet)
}

func serverMain(args []string) {
	flags := flag.NewFlagSet("server", flag.ExitOnError)
	help := flags.Bool("help", false, "show this help.")
	privateKey := flags.String("key",
		filepath.Join(guessHomeDir(), ".ssh", "id_rsa"),
		"a path to private key file.")
	usePasswd := flags.Bool("use-passwd",
		false,
		"use password authentication. use public key authentication by default.")
	userName := flags.String("user",
		guessUserName(),
		"ssh user name")
	clipFifo := flags.String("clip",
		"$HOME/clip",
		"a remote path to a fifo.")
	netclipCmd := flags.String("netclip",
		"netclip",
		"a remote path to netclip executable. search through $PATH by default.")
	err := flags.Parse(args)

	if err != nil {
		showServerUsage(os.Stderr, flags)
		os.Exit(1)
	} else if *help {
		showServerUsage(os.Stdout, flags)
		os.Exit(0)
	} else if flags.NArg() == 0 {
		fmt.Fprint(os.Stderr, "Remote address must be specified.\n\n")
		showServerUsage(os.Stderr, flags)
		os.Exit(1)
	}

	remoteAddr := flags.Arg(0)

	clipWriterMaker, ok := clipWriterMakers[runtime.GOOS]
	if !ok {
		log.Fatal("Unsupported host OS: ", runtime.GOOS)
	}
	clipWriter := clipWriterMaker()

	var authMethod ssh.AuthMethod

	if *usePasswd {
		authMethod = ssh.PasswordCallback(passwordCallback)
	} else {
		privateKeyBytes, err := ioutil.ReadFile(*privateKey)
		if err != nil {
			log.Fatal("failed to read key file: ", err)
		}
		privateKeySigner, err := ssh.ParsePrivateKey(privateKeyBytes)
		if err != nil {
			log.Fatal("failed to parse key: ", err)
		}
		authMethod = ssh.PublicKeys(privateKeySigner)
	}

	config := &ssh.ClientConfig{
		User: *userName,
		Auth: []ssh.AuthMethod{ authMethod },
	}

	log.Println("connecting to", remoteAddr)
	client, err := ssh.Dial("tcp4", remoteAddr, config)
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
					clipWriter.Write(buf.Bytes())
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

type WriterFunc func ([]byte) (int, error)
func (f WriterFunc) Write(data []byte) (int, error) {
	return f(data)
}

func makeStdinWriter(command string, args ...string) io.Writer {
	return WriterFunc(func (data []byte) (int, error) {
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
	})
}

var (
	clipWriterMakers = map[string]func() io.Writer {
		"windows": func() io.Writer {
			return transform.NewWriter(
				makeStdinWriter(`C:\Windows\System32\clip.exe`),
				japanese.ShiftJIS.NewEncoder())
		},
		"darwin": func() io.Writer {
			return makeStdinWriter("/usr/bin/pbcopy")
		},
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

	stdinChan := make(chan byte, 1)
	clipChan := make(chan byte, 1024)

	go func() {
		err := readLoop(os.Stdin, stdinChan)
		if err != io.EOF {
			log.Fatal(err)
		}
	}()

	go func() {
		clip, err := os.Open(flags.Arg(0))
		if err != nil {
			log.Fatal("failed to open clip fifo: ", err)
		}

		err = readLoop(clip, clipChan)
		if err != io.EOF {
			log.Fatal(err)
		}
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
